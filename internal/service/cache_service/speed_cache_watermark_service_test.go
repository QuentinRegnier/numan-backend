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

func TestSpeedCacheWatermarkService(t *testing.T) {
	ctx := context.Background()

	t.Run("SetWatermarkInSpeedCache and GetWatermarksFromSpeedCache", func(t *testing.T) {
		srv := test.SetupTestRedis(t)

		require.NoError(t, SetWatermarkInSpeedCache(ctx, 10, 100, 5000))
		require.NoError(t, SetWatermarkInSpeedCache(ctx, 10, 200, 6000))
		// Add invalid field to verify strict parsing skips invalid ints
		require.NoError(t, redis.ConvWatermarks.HSet(ctx, 10, "invalid", "bad"))

		wm, err := GetWatermarksFromSpeedCache(ctx, 10)
		require.NoError(t, err)
		assert.Equal(t, map[int64]int64{100: 5000, 200: 6000}, wm)

		// RefreshTTL failure logs warning but succeeds
		srv.SetFailCmd("EXPIRE", errors.New("expire warn"))
		require.NoError(t, SetWatermarkInSpeedCache(ctx, 10, 100, 5001))
		wm, err = GetWatermarksFromSpeedCache(ctx, 10)
		require.NoError(t, err)
		assert.Equal(t, int64(5001), wm[100])
		srv.SetFailCmd("EXPIRE", nil)
	})

	t.Run("Error Paths", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		var appErr *numan_error.AppError

		srv.SetFailCmd("HSET", errors.New("hset err"))
		require.ErrorAs(t, SetWatermarkInSpeedCache(ctx, 10, 100, 5000), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("HSET", nil)

		srv.SetFailCmd("HGETALL", errors.New("hgetall err"))
		_, err := GetWatermarksFromSpeedCache(ctx, 10)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
	})
}
