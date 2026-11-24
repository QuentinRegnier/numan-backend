package redis_test

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisHealthAndCoreHelpers(t *testing.T) {
	t.Run("collapsed single tier pings cleanly", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		ctx := context.Background()

		require.NoError(t, repoRedis.PingCore(ctx))
		require.NoError(t, repoRedis.PingObjects(ctx))
		require.NoError(t, repoRedis.PingPubSub(ctx))
		assert.False(t, repoRedis.HasDedicatedObjectPool())
		assert.False(t, repoRedis.HasDedicatedPubSub())

		coreT, objT, psT := repoRedis.CollectAllTiersTelemetry(ctx)
		assert.Equal(t, "core", coreT.Tier)
		assert.Equal(t, "objects", objT.Tier)
		assert.Equal(t, "pubsub", psT.Tier)
	})

	t.Run("multi tier detects dedicated object pool and pubsub", func(t *testing.T) {
		_, _, _ = test.SetupTestRedisMultiTier(t)
		ctx := context.Background()

		require.NoError(t, repoRedis.PingCore(ctx))
		require.NoError(t, repoRedis.PingObjects(ctx))
		require.NoError(t, repoRedis.PingPubSub(ctx))
		assert.True(t, repoRedis.HasDedicatedObjectPool())
		assert.True(t, repoRedis.HasDedicatedPubSub())

		require.NoError(t, repoRedis.RPushOnCore(ctx, "test:list", "a", "b"))
		n, err := repoRedis.LLenOnCore(ctx, "test:list")
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		popped, err := repoRedis.LPopOnCore(ctx, "test:list")
		require.NoError(t, err)
		assert.Equal(t, "a", popped)
	})
}
