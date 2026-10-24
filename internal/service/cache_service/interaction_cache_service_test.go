package cache_service

import (
	"context"
	"errors"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
)

func TestInteractionCacheService(t *testing.T) {
	ctx := context.Background()

	t.Run("Post and Comment Like Idempotency Add and Remove", func(t *testing.T) {
		_ = test.SetupTestRedis(t)

		// Post target (0)
		assert.True(t, TryAddLikeIdempotency(ctx, variables.LikeTargetTypePost, 100, 1))
		assert.False(t, TryAddLikeIdempotency(ctx, variables.LikeTargetTypePost, 100, 1)) // Duplicate
		assert.True(t, TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypePost, 100, 1))
		assert.False(t, TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypePost, 100, 1)) // Already removed

		// Comment target (1)
		assert.True(t, TryAddLikeIdempotency(ctx, variables.LikeTargetTypeComment, 200, 2))
		assert.False(t, TryAddLikeIdempotency(ctx, variables.LikeTargetTypeComment, 200, 2))
		assert.True(t, TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypeComment, 200, 2))
		assert.False(t, TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypeComment, 200, 2))
	})

	t.Run("Unknown Target Type and Redis Errors return false safely without panicking", func(t *testing.T) {
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
