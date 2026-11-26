package worker

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/metrics"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestSnowflakeID(ts time.Time, seq int64) int64 {
	deltaMs := ts.UTC().UnixMilli() - variables.Epoch
	return (deltaMs << variables.TimeShift) | (1 << variables.NodeShift) | (seq & variables.StepMax)
}

func TestRedisOrphanCleanupWorker_DryRunAndLiveModes(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 26, 4, 0, 0, 0, time.UTC)

	oldAliveUserID := makeTestSnowflakeID(now.Add(-2*time.Hour), 1)
	oldOrphanUserID := makeTestSnowflakeID(now.Add(-3*time.Hour), 2)
	recentWriteBehindUserID := makeTestSnowflakeID(now.Add(-3*time.Minute), 3) // Inside 15m grace window
	futureSkewUserID := makeTestSnowflakeID(now.Add(2*time.Minute), 4)         // Clock skew -> must be skipped

	oldTombstonePostID := makeTestSnowflakeID(now.Add(-48*time.Hour), 5)
	oldPurgedPostID := makeTestSnowflakeID(now.Add(-72*time.Hour), 6)

	t.Run("DryRun detects orphans and records metrics without unlinking any key", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		reg := metrics.NewRegistry()

		require.NoError(t, redis.UsersLite.SetPrimitive(ctx, oldAliveUserID, "alive"))
		require.NoError(t, redis.UsersLite.SetPrimitive(ctx, oldOrphanUserID, "orphan"))
		require.NoError(t, redis.UsersLite.SetPrimitive(ctx, recentWriteBehindUserID, "recent"))
		require.NoError(t, redis.UsersLite.SetPrimitive(ctx, futureSkewUserID, "future"))
		require.NoError(t, redis.UsersLite.SetPrimitive(ctx, "composite:subkey", "ignored"))

		cfg := RedisOrphanCleanupConfig{
			DryRun:            true,
			GraceWindow:       15 * time.Minute,
			ScanCount:         10,
			PostgresBatchSize: 100,
			UnlinkBatchSize:   50,
			MaxKeysPerCycle:   1000,
			Targets: []RedisOrphanTarget{
				{Name: "UsersLite", Collection: redis.UsersLite, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityUser},
			},
			NowFn: func() time.Time { return now },
			CheckExistingFn: func(_ context.Context, entity string, ids []int64) (map[int64]bool, error) {
				assert.Equal(t, postgres.OrphanEntityUser, entity)
				// Recent & future & composite keys must never even be queried in Postgres
				assert.ElementsMatch(t, []int64{oldAliveUserID, oldOrphanUserID}, ids)
				return map[int64]bool{oldAliveUserID: true}, nil
			},
			MetricsRegistry: reg,
		}

		summary, err := processRedisOrphanCleanup(ctx, cfg)
		require.NoError(t, err)
		assert.True(t, summary.DryRun)
		assert.Equal(t, int64(5), summary.KeysScanned)
		assert.Equal(t, int64(2), summary.SkippedGrace)
		assert.Equal(t, int64(1), summary.SkippedInvalid)
		assert.Equal(t, int64(1), summary.OrphansDetected)
		assert.Equal(t, int64(0), summary.OrphansUnlinked)
		assert.Equal(t, int64(0), summary.Errors)

		// Dry-run invariant: orphan key MUST still exist in Redis
		exists, err := redis.UsersLite.Exists(ctx, oldOrphanUserID)
		require.NoError(t, err)
		assert.True(t, exists)

		rendered := reg.Render(ctx)
		assert.Contains(t, rendered, `numan_redis_orphan_scanned_total{tier="core",entity="user"} 5`)
		assert.Contains(t, rendered, `numan_redis_orphan_skipped_grace_total{tier="core",entity="user"} 2`)
		assert.Contains(t, rendered, `numan_redis_orphan_detected_total{tier="core",entity="user"} 1`)
		assert.Contains(t, rendered, `numan_redis_orphan_unlinked_total{tier="core",entity="user"} 0`)
	})

	t.Run("Live mode unlinks only verified orphans across Core and Object Pool and preserves Posts tombstone", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		reg := metrics.NewRegistry()

		require.NoError(t, redis.UsersLite.SetPrimitive(ctx, oldAliveUserID, "alive"))
		require.NoError(t, redis.UsersLite.SetPrimitive(ctx, oldOrphanUserID, "orphan"))
		require.NoError(t, redis.UsersLite.SetPrimitive(ctx, recentWriteBehindUserID, "recent"))
		require.NoError(t, redis.Posts.SetPrimitive(ctx, oldTombstonePostID, "tombstone"))
		require.NoError(t, redis.Posts.SetPrimitive(ctx, oldPurgedPostID, "purged"))
		require.NoError(t, redis.PostLikesSet.SAdd(ctx, oldTombstonePostID, int64(1)))

		cfg := RedisOrphanCleanupConfig{
			DryRun:            false,
			GraceWindow:       15 * time.Minute,
			ScanCount:         10,
			PostgresBatchSize: 100,
			UnlinkBatchSize:   1, // Force multiple UNLINK chunks
			MaxKeysPerCycle:   1000,
			Targets: []RedisOrphanTarget{
				{Name: "UsersLite", Collection: redis.UsersLite, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityUser},
				{Name: "Posts", Collection: redis.Posts, Tier: redisOrphanTierObject, Entity: postgres.OrphanEntityPostPhysical},
				{Name: "PostLikesSet", Collection: redis.PostLikesSet, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityPostActive},
			},
			NowFn: func() time.Time { return now },
			CheckExistingFn: func(_ context.Context, entity string, ids []int64) (map[int64]bool, error) {
				switch entity {
				case postgres.OrphanEntityUser:
					return map[int64]bool{oldAliveUserID: true}, nil
				case postgres.OrphanEntityPostPhysical:
					// Soft-deleted tombstone still physically exists in content.posts; purged post does not.
					return map[int64]bool{oldTombstonePostID: true}, nil
				case postgres.OrphanEntityPostActive:
					// Soft-deleted post is NOT active -> derived PostLikesSet key is an orphan!
					return map[int64]bool{}, nil
				default:
					return nil, errors.New("unexpected entity: " + entity)
				}
			},
			MetricsRegistry: reg,
		}

		summary, err := processRedisOrphanCleanup(ctx, cfg)
		require.NoError(t, err)
		assert.False(t, summary.DryRun)
		assert.Equal(t, int64(3), summary.OrphansDetected)
		assert.Equal(t, int64(3), summary.OrphansUnlinked)
		assert.Equal(t, int64(0), summary.Errors)

		// Verified orphans unlinked
		exists, _ := redis.UsersLite.Exists(ctx, oldOrphanUserID)
		assert.False(t, exists, "orphan UsersLite key must be unlinked")
		exists, _ = redis.Posts.Exists(ctx, oldPurgedPostID)
		assert.False(t, exists, "physically purged Post key must be unlinked")
		exists, _ = redis.PostLikesSet.Exists(ctx, oldTombstonePostID)
		assert.False(t, exists, "derived PostLikesSet of soft-deleted post must be unlinked")

		// Protected keys preserved
		exists, _ = redis.UsersLite.Exists(ctx, oldAliveUserID)
		assert.True(t, exists, "live user must stay")
		exists, _ = redis.UsersLite.Exists(ctx, recentWriteBehindUserID)
		assert.True(t, exists, "recent Write-Behind user in grace window must stay")
		exists, _ = redis.Posts.Exists(ctx, oldTombstonePostID)
		assert.True(t, exists, "Posts tombstone must be preserved")
	})

	t.Run("Fail-closed: Postgres error never triggers UNLINK", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		reg := metrics.NewRegistry()

		require.NoError(t, redis.UsersLite.SetPrimitive(ctx, oldOrphanUserID, "orphan"))

		cfg := RedisOrphanCleanupConfig{
			DryRun:            false,
			GraceWindow:       15 * time.Minute,
			ScanCount:         10,
			PostgresBatchSize: 100,
			UnlinkBatchSize:   50,
			MaxKeysPerCycle:   1000,
			Targets: []RedisOrphanTarget{
				{Name: "UsersLite", Collection: redis.UsersLite, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityUser},
			},
			NowFn: func() time.Time { return now },
			CheckExistingFn: func(_ context.Context, _ string, _ []int64) (map[int64]bool, error) {
				return nil, errors.New("temporary replica timeout")
			},
			MetricsRegistry: reg,
		}

		summary, err := processRedisOrphanCleanup(ctx, cfg)
		require.NoError(t, err)
		assert.Equal(t, int64(0), summary.OrphansDetected)
		assert.Equal(t, int64(0), summary.OrphansUnlinked)
		assert.Equal(t, int64(1), summary.Errors)

		exists, err := redis.UsersLite.Exists(ctx, oldOrphanUserID)
		require.NoError(t, err)
		assert.True(t, exists, "key MUST NOT be unlinked when Postgres check fails")

		assert.Contains(t, reg.Render(ctx), `numan_redis_orphan_errors_total{stage="postgres"} 1`)
	})

	t.Run("Respects MaxKeysPerCycle budget", func(t *testing.T) {
		_ = test.SetupTestRedis(t)

		for i := 1; i <= 10; i++ {
			id := makeTestSnowflakeID(now.Add(-2*time.Hour), int64(i))
			require.NoError(t, redis.UsersLite.SetPrimitive(ctx, id, i))
		}

		cfg := RedisOrphanCleanupConfig{
			DryRun:            true,
			GraceWindow:       15 * time.Minute,
			ScanCount:         3,
			PostgresBatchSize: 50,
			UnlinkBatchSize:   50,
			MaxKeysPerCycle:   4,
			Targets: []RedisOrphanTarget{
				{Name: "UsersLite", Collection: redis.UsersLite, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityUser},
			},
			NowFn: func() time.Time { return now },
			CheckExistingFn: func(_ context.Context, _ string, ids []int64) (map[int64]bool, error) {
				return map[int64]bool{}, nil
			},
		}

		summary, err := processRedisOrphanCleanup(ctx, cfg)
		require.NoError(t, err)
		assert.True(t, summary.BudgetReached)
		assert.Equal(t, int64(4), summary.KeysScanned)
	})

	t.Run("End-to-end integration with real postgres.FuncFilterExistingEntityIDs mock", func(t *testing.T) {
		_ = test.SetupTestRedis(t)

		require.NoError(t, redis.POIs.SetPrimitive(ctx, oldAliveUserID, "live-poi"))
		require.NoError(t, redis.POIs.SetPrimitive(ctx, oldOrphanUserID, "deleted-poi"))

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "util.func_filter_existing_entity_ids")
			assert.Equal(t, postgres.OrphanEntityPOIActive, args[0].Value)
			return test.NewMockRows([]string{"id"}, [][]driver.Value{
				{oldAliveUserID},
			}), nil
		}, nil)

		cfg := RedisOrphanCleanupConfig{
			DryRun:            false,
			GraceWindow:       15 * time.Minute,
			ScanCount:         10,
			PostgresBatchSize: 50,
			UnlinkBatchSize:   50,
			MaxKeysPerCycle:   100,
			Targets: []RedisOrphanTarget{
				{Name: "POIs", Collection: redis.POIs, Tier: redisOrphanTierObject, Entity: postgres.OrphanEntityPOIActive},
			},
			NowFn: func() time.Time { return now },
		}

		summary, err := processRedisOrphanCleanup(ctx, cfg)
		require.NoError(t, err)
		assert.Equal(t, int64(1), summary.OrphansDetected)
		assert.Equal(t, int64(1), summary.OrphansUnlinked)

		existsAlive, _ := redis.POIs.Exists(ctx, oldAliveUserID)
		existsOrphan, _ := redis.POIs.Exists(ctx, oldOrphanUserID)
		assert.True(t, existsAlive)
		assert.False(t, existsOrphan)
	})

	t.Run("DefaultRedisOrphanTargets covers 31 collections with TTL=0 priority wave and Posts tombstone protection", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		targets := DefaultRedisOrphanTargets()
		assert.Len(t, targets, 31)

		var postsTarget *RedisOrphanTarget
		for i := range targets {
			require.NotNil(t, targets[i].Collection, "target %s must be initialized", targets[i].Name)
			if targets[i].Name == "Posts" {
				postsTarget = &targets[i]
			}
		}
		require.NotNil(t, postsTarget)
		assert.Equal(t, postgres.OrphanEntityPostPhysical, postsTarget.Entity)
		assert.Equal(t, redisOrphanTierObject, postsTarget.Tier)
	})
}
