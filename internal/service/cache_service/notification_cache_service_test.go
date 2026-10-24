package cache_service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotificationCacheService(t *testing.T) {
	ctx := context.Background()

	t.Run("AddNotificationToZSET, GetNotificationIDsFromZSET, PurgeNotificationsZSET, TouchActivityTimestamp", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		userID := int64(15)

		require.NoError(t, AddNotificationToZSET(ctx, userID, 1001, 1000))
		require.NoError(t, AddNotificationToZSET(ctx, userID, 1002, 2000))

		ids, err := GetNotificationIDsFromZSET(ctx, userID, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []int64{1002, 1001}, ids)

		// Best-effort RefreshTTL on GetNotificationIDsFromZSET logs warning and still returns IDs
		srv.SetFailCmd("EXPIRE", errors.New("expire warn"))
		ids, err = GetNotificationIDsFromZSET(ctx, userID, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []int64{1002, 1001}, ids)
		srv.SetFailCmd("EXPIRE", nil)

		// Purge
		require.NoError(t, PurgeNotificationsZSET(ctx, userID))
		ids, err = GetNotificationIDsFromZSET(ctx, userID, 0, 10)
		require.NoError(t, err)
		assert.Empty(t, ids)

		// TouchActivityTimestamp
		ts := TouchActivityTimestamp(ctx, userID)
		assert.Greater(t, ts, int64(0))

		srv.SetFailCmd("SET", errors.New("set warn"))
		ts = TouchActivityTimestamp(ctx, userID)
		assert.Greater(t, ts, int64(0))
		srv.SetFailCmd("SET", nil)
	})

	t.Run("Error Paths return wrapped AppError", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		var appErr *numan_error.AppError

		// EVAL failure in AddNotificationToZSET
		srv.SetFailCmd("EVAL", errors.New("eval err"))
		err := AddNotificationToZSET(ctx, 1, 100, 1000)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusInternalServerError, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("EVAL", nil)

		// EXPIRE failure in AddNotificationToZSET
		srv.SetFailCmd("EXPIRE", errors.New("expire err"))
		err = AddNotificationToZSET(ctx, 1, 100, 1000)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("EXPIRE", nil)

		// ZREVRANGE failure in GetNotificationIDsFromZSET
		srv.SetFailCmd("ZREVRANGE", errors.New("zrev err"))
		_, err = GetNotificationIDsFromZSET(ctx, 1, 0, 10)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("ZREVRANGE", nil)

		// DEL failure in PurgeNotificationsZSET
		srv.SetFailCmd("DEL", errors.New("del err"))
		err = PurgeNotificationsZSET(ctx, 1)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
	})
}
