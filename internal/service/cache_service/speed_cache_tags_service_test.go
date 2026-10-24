package cache_service

import (
	"context"
	"errors"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpeedCacheTagsService(t *testing.T) {
	ctx := context.Background()

	t.Run("StoreTagInSpeedCache and SearchTagsByPrefix", func(t *testing.T) {
		_ = test.SetupTestRedis(t)

		require.NoError(t, StoreTagInSpeedCache(ctx, "Golang"))
		require.NoError(t, StoreTagInSpeedCache(ctx, "Gopher"))

		tags, err := SearchTagsByPrefix(ctx, "go", 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{"golang", "gopher"}, tags)

		empty, err := SearchTagsByPrefix(ctx, "python", 0, 10)
		require.NoError(t, err)
		assert.Empty(t, empty)
	})

	t.Run("IsTagTrending", func(t *testing.T) {
		srv := test.SetupTestRedis(t)

		assert.False(t, IsTagTrending(ctx, "golang"))

		require.NoError(t, redis.HashtagLeaderboard.ZAdd(ctx, "global", 42.0, "golang"))
		assert.True(t, IsTagTrending(ctx, "golang"))

		srv.SetFailCmd("ZSCORE", errors.New("zscore err"))
		assert.False(t, IsTagTrending(ctx, "golang"))
		srv.SetFailCmd("ZSCORE", nil)
	})

	t.Run("Error Paths", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		var appErr *numan_error.AppError

		srv.SetFailCmd("ZADD", errors.New("zadd err"))
		require.ErrorAs(t, StoreTagInSpeedCache(ctx, "fail"), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("ZADD", nil)

		srv.SetFailCmd("ZRANGEBYLEX", errors.New("lex err"))
		_, err := SearchTagsByPrefix(ctx, "fa", 0, 10)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
	})
}
