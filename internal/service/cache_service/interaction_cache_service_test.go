package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestInteractionCacheService(t *testing.T) {
	ctx := context.Background()

	t.Run("Post and Comment Like Idempotency Add, Remove, and LongTTL application", func(t *testing.T) {
		defer test.ResetCollections()
		_ = test.SetupTestRedis(t)

		// Post target (0)
		assert.True(t, TryAddLikeIdempotency(ctx, variables.LikeTargetTypePost, 100, 1))
		ttlPost, err := redis.PostLikesSet.TTL(ctx, int64(100))
		require.NoError(t, err)
		assert.Greater(t, ttlPost, 29*24*time.Hour, "PostLikesSet must carry LongTTL (30d)")
		assert.LessOrEqual(t, ttlPost, variables.LongTTL)

		assert.False(t, TryAddLikeIdempotency(ctx, variables.LikeTargetTypePost, 100, 1)) // Duplicate
		assert.True(t, TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypePost, 100, 1))
		assert.False(t, TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypePost, 100, 1)) // Already removed

		// Comment target (1)
		assert.True(t, TryAddLikeIdempotency(ctx, variables.LikeTargetTypeComment, 200, 2))
		ttlComment, err := redis.CommentLikesSet.TTL(ctx, int64(200))
		require.NoError(t, err)
		assert.Greater(t, ttlComment, 29*24*time.Hour, "CommentLikesSet must carry LongTTL (30d)")
		assert.LessOrEqual(t, ttlComment, variables.LongTTL)

		assert.False(t, TryAddLikeIdempotency(ctx, variables.LikeTargetTypeComment, 200, 2))
		assert.True(t, TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypeComment, 200, 2))
		assert.False(t, TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypeComment, 200, 2))
	})

	t.Run("Cold miss rehydration from L2 MongoDB blocks double-like and allows unlike after expiry", func(t *testing.T) {
		defer test.ResetCollections()
		_ = test.SetupTestRedis(t)
		mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

		mt.Run("blocks duplicate like after Redis key expired", func(mt *mtest.T) {
			mongo.Likes = test.NewMockCollection(mt, "likes", nil)
			mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.likes", mtest.FirstBatch,
				bson.D{{Key: "user_id", Value: int64(42)}},
				bson.D{{Key: "user_id", Value: int64(99)}},
			))

			// Key expired in Redis -> must rehydrate {42, 99} from L2 Mongo and block duplicate like by 42
			assert.False(mt, TryAddLikeIdempotency(ctx, variables.LikeTargetTypePost, 500, 42))
			ttl, err := redis.PostLikesSet.TTL(ctx, int64(500))
			require.NoError(mt, err)
			assert.Greater(mt, ttl, 29*24*time.Hour)

			// User 77 never liked -> allowed in O(1) without touching Mongo again
			assert.True(mt, TryAddLikeIdempotency(ctx, variables.LikeTargetTypePost, 500, 77))
		})

		mt.Run("allows unlike after Redis key expired", func(mt *mtest.T) {
			mongo.Likes = test.NewMockCollection(mt, "likes", nil)
			mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.likes", mtest.FirstBatch,
				bson.D{{Key: "user_id", Value: int64(42)}},
				bson.D{{Key: "user_id", Value: int64(99)}},
			))

			// Key 501 absent from Redis -> rehydrates {42, 99} from L2 Mongo and allows 42 to unlike
			assert.True(mt, TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypePost, 501, 42))
			ttl, err := redis.PostLikesSet.TTL(ctx, int64(501))
			require.NoError(mt, err)
			assert.Greater(mt, ttl, 29*24*time.Hour)

			// Second unlike by 42 is blocked (already removed from rehydrated set)
			assert.False(mt, TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypePost, 501, 42))
			// User 99 (still in rehydrated set) can unlike in O(1)
			assert.True(mt, TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypePost, 501, 99))
		})
	})

	t.Run("Cold miss fallback to L3 PostgreSQL when L2 Mongo is unavailable", func(t *testing.T) {
		defer test.ResetCollections()
		_ = test.SetupTestRedis(t)
		mongo.Likes = nil

		cols := []string{"id", "target_type", "target_id", "user_id", "created_at"}
		now := domain.NowTime()

		pgCalls := 0
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			pgCalls++
			require.Contains(t, query, "content.func_load_likes")
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(1), 1, int64(888), int64(15), now},
				{int64(2), 1, int64(888), int64(16), now},
			}), nil
		}, nil)

		// Comment 888 expired from Redis & Mongo nil -> rehydrates {15, 16} from L3 Postgres
		assert.False(t, TryAddLikeIdempotency(ctx, variables.LikeTargetTypeComment, 888, 15))
		assert.Equal(t, 1, pgCalls)
		ttl, err := redis.CommentLikesSet.TTL(ctx, int64(888))
		require.NoError(t, err)
		assert.Greater(t, ttl, 29*24*time.Hour)

		// Subsequent unlike by user 16 runs purely in Redis (no extra Postgres call)
		assert.True(t, TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypeComment, 888, 16))
		assert.Equal(t, 1, pgCalls)
	})

	t.Run("Unknown Target Type and Redis Errors return false safely without panicking", func(t *testing.T) {
		defer test.ResetCollections()
		srv := test.SetupTestRedis(t)

		// Unknown targetEntityType (e.g., 99) -> nil collection guard
		assert.False(t, TryAddLikeIdempotency(ctx, 99, 100, 1))
		assert.False(t, TryRemoveLikeIdempotency(ctx, 99, 100, 1))

		// Redis SADD / SREM errors
		srv.SetFailCmd("SADD", errors.New("sadd err"))
		assert.False(t, TryAddLikeIdempotency(ctx, variables.LikeTargetTypePost, 100, 1))
		srv.SetFailCmd("SADD", nil)

		srv.SetFailCmd("SREM", errors.New("srem err"))
		assert.False(t, TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypePost, 100, 1))
		srv.SetFailCmd("SREM", nil)
	})
}
