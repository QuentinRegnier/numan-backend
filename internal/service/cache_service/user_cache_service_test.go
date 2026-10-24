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

func TestUserCacheService(t *testing.T) {
	ctx := context.Background()

	t.Run("MarkUserTimelineEmpty, AddPostToUserProfile, GetTopUserPostIDs, RemovePostFromUserProfile, PurgeUserTimeline", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		userID := int64(10)

		// Cache Miss when ZSET does not exist
		_, err := GetTopUserPostIDs(ctx, userID, 0, 10)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)

		// Mark empty -> returns empty slice (Cache Hit on empty profile)
		require.NoError(t, MarkUserTimelineEmpty(ctx, userID))
		ids, err := GetTopUserPostIDs(ctx, userID, 0, 10)
		require.NoError(t, err)
		assert.Empty(t, ids)

		// Add posts -> removes empty marker and returns sorted post IDs
		require.NoError(t, AddPostToUserProfile(ctx, userID, 101, 1000))
		require.NoError(t, AddPostToUserProfile(ctx, userID, 102, 2000))

		ids, err = GetTopUserPostIDs(ctx, userID, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []int64{102, 101}, ids)

		// Remove post
		require.NoError(t, RemovePostFromUserProfile(ctx, userID, 102))
		ids, err = GetTopUserPostIDs(ctx, userID, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []int64{101}, ids)

		// Purge timeline
		require.NoError(t, PurgeUserTimeline(ctx, userID))

		// Warning logs on RefreshTTL and ZRem
		srv.SetFailCmd("EXPIRE", errors.New("expire warn"))
		require.NoError(t, MarkUserTimelineEmpty(ctx, userID))
		srv.SetFailCmd("EXPIRE", nil)

		srv.SetFailCmd("ZREM", errors.New("zrem warn"))
		require.NoError(t, AddPostToUserProfile(ctx, userID, 103, 3000))
		srv.SetFailCmd("ZREM", nil)
	})

	t.Run("Error Paths", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		var appErr *numan_error.AppError
		userID := int64(20)

		require.NoError(t, AddPostToUserProfile(ctx, userID, 201, 1000))

		srv.SetFailCmd("EXISTS", errors.New("exists err"))
		_, err := GetTopUserPostIDs(ctx, userID, 0, 10)
		require.ErrorAs(t, err, &appErr)
		srv.SetFailCmd("EXISTS", nil)

		srv.SetFailCmd("ZREVRANGE", errors.New("zrev err"))
		_, err = GetTopUserPostIDs(ctx, userID, 0, 10)
		require.ErrorAs(t, err, &appErr)
		srv.SetFailCmd("ZREVRANGE", nil)

		srv.SetFailCmd("ZADD", errors.New("zadd err"))
		require.ErrorAs(t, MarkUserTimelineEmpty(ctx, userID), &appErr)
		require.ErrorAs(t, AddPostToUserProfile(ctx, userID, 202, 2000), &appErr)
		srv.SetFailCmd("ZADD", nil)

		srv.SetFailCmd("ZREM", errors.New("zrem err"))
		require.ErrorAs(t, RemovePostFromUserProfile(ctx, userID, 201), &appErr)
		srv.SetFailCmd("ZREM", nil)

		srv.SetFailCmd("DEL", errors.New("del err"))
		require.ErrorAs(t, PurgeUserTimeline(ctx, userID), &appErr)
	})
}
