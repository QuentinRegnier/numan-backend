package cache_service_test

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthCacheService(t *testing.T) {
	t.Run("collapsed single tier pings all three logical tiers", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		ctx := context.Background()

		require.NoError(t, cache_service.PingRedisCore(ctx))
		require.NoError(t, cache_service.PingRedisObjects(ctx))
		require.NoError(t, cache_service.PingRedisPubSub(ctx))
		assert.False(t, cache_service.HasDedicatedObjectPool())
		assert.False(t, cache_service.HasDedicatedPubSub())
	})

	t.Run("multi-tier reports dedicated object pool and pubsub", func(t *testing.T) {
		_, _, _ = test.SetupTestRedisMultiTier(t)
		ctx := context.Background()

		require.NoError(t, cache_service.PingRedisCore(ctx))
		require.NoError(t, cache_service.PingRedisObjects(ctx))
		require.NoError(t, cache_service.PingRedisPubSub(ctx))
		assert.True(t, cache_service.HasDedicatedObjectPool())
		assert.True(t, cache_service.HasDedicatedPubSub())
	})
}
