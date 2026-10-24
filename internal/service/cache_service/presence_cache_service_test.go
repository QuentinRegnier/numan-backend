package cache_service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/sync_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPresenceCacheService(t *testing.T) {
	ctx := context.Background()

	t.Run("MarkUserOnline, IsUserOnline, and AreUsersOnline", func(t *testing.T) {
		_ = test.SetupTestRedis(t)

		assert.False(t, IsUserOnline(ctx, 10))
		require.NoError(t, MarkUserOnline(ctx, 10))
		assert.True(t, IsUserOnline(ctx, 10))

		// Empty input returns empty map
		emptyMap, err := AreUsersOnline(ctx, sync_models.SyncPresenceInput{UserIDs: nil})
		require.NoError(t, err)
		assert.Empty(t, emptyMap)

		// Batch check for online (10) and offline (20)
		resMap, err := AreUsersOnline(ctx, sync_models.SyncPresenceInput{UserIDs: []int64{10, 20}})
		require.NoError(t, err)
		assert.True(t, resMap[10])
		assert.False(t, resMap[20])
	})

	t.Run("Error Paths", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		var appErr *numan_error.AppError

		// SET error in MarkUserOnline
		srv.SetFailCmd("SET", errors.New("set err"))
		require.ErrorAs(t, MarkUserOnline(ctx, 1), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("SET", nil)

		// EXPIRE error in MarkUserOnline
		srv.SetFailCmd("EXPIRE", errors.New("expire err"))
		require.ErrorAs(t, MarkUserOnline(ctx, 1), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("EXPIRE", nil)

		// EXISTS error in IsUserOnline returns false safely
		srv.SetFailCmd("EXISTS", errors.New("exists err"))
		assert.False(t, IsUserOnline(ctx, 1))
		srv.SetFailCmd("EXISTS", nil)

		// ListLimitVerifDefault error propagated without double wrapping
		tooMany := make([]int64, 10001)
		_, err := AreUsersOnline(ctx, sync_models.SyncPresenceInput{UserIDs: tooMany})
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus)

		// MGET error in AreUsersOnline
		srv.SetFailCmd("MGET", errors.New("mget err"))
		_, err = AreUsersOnline(ctx, sync_models.SyncPresenceInput{UserIDs: []int64{1, 2}})
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
	})
}
