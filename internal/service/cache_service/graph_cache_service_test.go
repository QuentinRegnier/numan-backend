package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func TestGraphCacheService(t *testing.T) {
	ctx := context.Background()

	t.Run("UpdateTagCooccurrences and GetRelatedTagsLazy with Markov decay and lazy pruning", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		nowMs := time.Now().UnixMilli()

		// Fewer than 2 tags is a no-op
		UpdateTagCooccurrences(ctx, []string{"only_one"}, nowMs)
		assert.Nil(t, GetRelatedTagsLazy(ctx, "only_one"))

		// Update co-occurrences for 2 tags
		UpdateTagCooccurrences(ctx, []string{"  GoLang ", "Redis", ""}, nowMs-3600_000)
		// Second update in the past (anti-time-travel deltaDays < 0 branch)
		UpdateTagCooccurrences(ctx, []string{"golang", "redis"}, nowMs-7200_000)

		related := GetRelatedTagsLazy(ctx, "Golang")
		require.NotNil(t, related)
		assert.Greater(t, related["redis"], 0.0)

		// Inject a dead edge (very old timestamp -> weight decays below GraphSurvivalEps) and a corrupted edge
		deadEdgeBytes, err := msgpack.Marshal(&tagEdge{
			Weight:    0.0001,
			Timestamp: nowMs - int64(365*24*3600*1000),
		})
		require.NoError(t, err)
		require.NoError(t, redis.GraphEdges.HSet(ctx, "golang", "dead_tag", deadEdgeBytes))
		require.NoError(t, redis.GraphEdges.HSet(ctx, "golang", "corrupt_tag", []byte("invalid-msgpack")))

		// Corrupted edge on updateEdge logs error and returns safely
		updateEdge(ctx, "golang", "corrupt_tag", nowMs)

		relatedAfter := GetRelatedTagsLazy(ctx, "golang")
		require.NotNil(t, relatedAfter)
		assert.Contains(t, relatedAfter, "redis")
		assert.NotContains(t, relatedAfter, "dead_tag")
		assert.NotContains(t, relatedAfter, "corrupt_tag")

		// Wait briefly for async lazy pruning goroutine (HDel)
		time.Sleep(20 * time.Millisecond)
		_, errDead := redis.GraphEdges.HGet(ctx, "golang", "dead_tag").Bytes()
		assert.Error(t, errDead)
	})

	t.Run("Error Paths in updateEdge and GetRelatedTagsLazy", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		nowMs := time.Now().UnixMilli()

		// HGET non-Nil error in updateEdge
		srv.SetFailCmd("HGET", errors.New("hget err"))
		updateEdge(ctx, "a", "b", nowMs)
		srv.SetFailCmd("HGET", nil)

		// HSET error in updateEdge
		srv.SetFailCmd("HSET", errors.New("hset err"))
		updateEdge(ctx, "a", "b", nowMs)
		srv.SetFailCmd("HSET", nil)

		// HGETALL error in GetRelatedTagsLazy
		srv.SetFailCmd("HGETALL", errors.New("hgetall err"))
		assert.Nil(t, GetRelatedTagsLazy(ctx, "a"))
		srv.SetFailCmd("HGETALL", nil)

		// HDEL error in async lazy pruning goroutine
		deadEdgeBytes, _ := msgpack.Marshal(&tagEdge{Weight: 0.00001, Timestamp: 1000})
		require.NoError(t, redis.GraphEdges.HSet(ctx, "prune_src", "dead", deadEdgeBytes))
		srv.SetFailCmd("HDEL", errors.New("hdel err"))
		_ = GetRelatedTagsLazy(ctx, "prune_src")
		time.Sleep(20 * time.Millisecond)
		srv.SetFailCmd("HDEL", nil)
	})

	t.Run("SeedGraphCache", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		now := time.Now()

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(
				[]string{"id", "hashtags", "indirect_hashtags", "created_at"},
				[][]driver.Value{
					{int64(1), `{ai,ml}`, `{tech}`, now},
				},
			), nil
		}, nil)

		require.NoError(t, SeedGraphCache(ctx))
		related := GetRelatedTagsLazy(ctx, "ai")
		require.NotNil(t, related)
		assert.Greater(t, related["ml"], 0.0)

		// Postgres error is propagated directly without double wrapping
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("pg down")
		}, nil)

		err := SeedGraphCache(ctx)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeDatabaseError, appErr.Code)
	})
}
