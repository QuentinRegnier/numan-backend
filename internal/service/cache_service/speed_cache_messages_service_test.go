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
)

func TestGetMessageIDsFromSpeedCache(t *testing.T) {
	ctx := context.Background()

	t.Run("L1 Hit for top and bottom scroll directions with frozen ceiling", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		convID := int64(10)

		for _, id := range []int64{10, 20, 30, 40, 50} {
			require.NoError(t, redis.MessagesIndex.ZAdd(ctx, convID, float64(id), id))
		}

		// Top scroll with frozen ceiling 40 and offset 50 (anti-cheat caps at 40)
		ids, err := GetMessageIDsFromSpeedCache(ctx, convID, 50, 2, "top", 40)
		require.NoError(t, err)
		assert.Equal(t, []int64{40, 30}, ids)

		// Top scroll with offset 40 (< frozen 50 -> exclusive "(40")
		ids, err = GetMessageIDsFromSpeedCache(ctx, convID, 40, 2, "top", 50)
		require.NoError(t, err)
		assert.Equal(t, []int64{30, 20}, ids)

		// Bottom scroll with offset 10 and frozen 30
		ids, err = GetMessageIDsFromSpeedCache(ctx, convID, 10, 2, "bottom", 30)
		require.NoError(t, err)
		assert.Equal(t, []int64{20, 30}, ids)
	})

	t.Run("Cache Hole / Redis Error triggers L3 Postgres Fallback and Async Rehydration", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		convID := int64(20)

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows([]string{"id"}, [][]driver.Value{{int64(101)}, {int64(102)}}), nil
		}, nil)

		srv.SetFailCmd("ZREVRANGEBYSCORE", errors.New("redis err"))
		ids, err := GetMessageIDsFromSpeedCache(ctx, convID, 0, 5, "top", 0)
		require.NoError(t, err)
		assert.Equal(t, []int64{101, 102}, ids)
		time.Sleep(20 * time.Millisecond)
		srv.SetFailCmd("ZREVRANGEBYSCORE", nil)

		// Test async rehydration error logging
		srv.SetFailCmd("ZADD", errors.New("zadd warn"))
		srv.SetFailCmd("EXPIRE", errors.New("expire warn"))
		ids, err = GetMessageIDsFromSpeedCache(ctx, convID, 0, 5, "top", 0)
		require.NoError(t, err)
		assert.Equal(t, []int64{101, 102}, ids)
		time.Sleep(20 * time.Millisecond)
	})

	t.Run("L3 Postgres Error propagated directly", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("pg down")
		}, nil)

		_, err := GetMessageIDsFromSpeedCache(ctx, 99, 0, 5, "top", 0)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeDatabaseError, appErr.Code)
	})
}
