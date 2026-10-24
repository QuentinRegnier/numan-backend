package cache_service

import (
	"context"
	"errors"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncCacheService(t *testing.T) {
	ctx := context.Background()

	t.Run("RecordConversationMutation, RecordMessageMutation, and Pull Getters", func(t *testing.T) {
		srv := test.SetupTestRedis(t)

		require.NoError(t, RecordConversationMutation(ctx, 100, []int64{1, 2}))
		require.NoError(t, RecordMessageMutation(ctx, 100, 999, []int64{1, 2}))

		convIDs, err := GetModifiedConversationIDs(ctx, 1, 0)
		require.NoError(t, err)
		assert.Equal(t, []int64{100}, convIDs)

		msgIDs, err := GetModifiedMessageIDs(ctx, 100, 0)
		require.NoError(t, err)
		assert.Equal(t, []int64{999}, msgIDs)

		// RefreshTTL warning branch in RecordMessageMutation
		srv.SetFailCmd("EXPIRE", errors.New("expire warn"))
		require.NoError(t, RecordMessageMutation(ctx, 100, 1000, nil))
		srv.SetFailCmd("EXPIRE", nil)
	})

	t.Run("Error Paths", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		var appErr *numan_error.AppError

		srv.SetFailCmd("ZADD", errors.New("zadd err"))
		require.ErrorAs(t, RecordConversationMutation(ctx, 100, []int64{1}), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)

		require.ErrorAs(t, RecordMessageMutation(ctx, 100, 999, []int64{1}), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("ZADD", nil)

		srv.SetFailCmd("ZRANGEBYSCORE", errors.New("zrangebyscore err"))
		_, err := GetModifiedConversationIDs(ctx, 1, 0)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)

		_, err = GetModifiedMessageIDs(ctx, 100, 0)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
	})
}
