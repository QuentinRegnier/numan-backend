package cache_service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageReactionCacheService(t *testing.T) {
	ctx := context.Background()

	t.Run("User Reaction CRUD and Aggregated Counts", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		msgID := int64(500)
		userID := int64(42)

		// Cache Miss (goredis.Nil) returns ("", nil)
		emoji, err := GetUserReaction(ctx, msgID, userID)
		require.NoError(t, err)
		assert.Equal(t, "", emoji)

		// Set & Get reaction
		require.NoError(t, SetUserReaction(ctx, msgID, userID, "🔥"))
		emoji, err = GetUserReaction(ctx, msgID, userID)
		require.NoError(t, err)
		assert.Equal(t, "🔥", emoji)

		// Delete reaction
		require.NoError(t, DeleteUserReaction(ctx, msgID, userID))
		emoji, err = GetUserReaction(ctx, msgID, userID)
		require.NoError(t, err)
		assert.Equal(t, "", emoji)

		// Increment & Decrement counts
		require.NoError(t, IncrementReactionCount(ctx, msgID, "🔥", 2))
		require.NoError(t, IncrementReactionCount(ctx, msgID, "❤️", 1))
		// Inject invalid non-integer count to verify warning log & skip
		require.NoError(t, redis.MessageReactionCounts.HSet(ctx, msgID, "corrupt", "not-an-int"))

		counts, err := GetMessageReactionCounts(ctx, msgID)
		require.NoError(t, err)
		assert.Equal(t, map[string]int{"🔥": 2, "❤️": 1}, counts)

		// Decrement to 0 triggers auto HDel cleanup (also test HDel warning when HDel fails)
		srv.SetFailCmd("HDEL", errors.New("hdel warn"))
		require.NoError(t, IncrementReactionCount(ctx, msgID, "❤️", -1))
		srv.SetFailCmd("HDEL", nil)

		require.NoError(t, IncrementReactionCount(ctx, msgID, "🔥", -2))
		counts, err = GetMessageReactionCounts(ctx, msgID)
		require.NoError(t, err)
		assert.NotContains(t, counts, "🔥")
	})

	t.Run("Error Paths", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		var appErr *numan_error.AppError

		// Real Redis failure on GetUserReaction returns 500 CodeCacheError
		srv.SetFailCmd("HGET", errors.New("hget err"))
		_, err := GetUserReaction(ctx, 1, 1)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusInternalServerError, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("HGET", nil)

		srv.SetFailCmd("HSET", errors.New("hset err"))
		require.ErrorAs(t, SetUserReaction(ctx, 1, 1, "👍"), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("HSET", nil)

		srv.SetFailCmd("HDEL", errors.New("hdel err"))
		require.ErrorAs(t, DeleteUserReaction(ctx, 1, 1), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("HDEL", nil)

		srv.SetFailCmd("HINCRBY", errors.New("hincrby err"))
		require.ErrorAs(t, IncrementReactionCount(ctx, 1, "👍", 1), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("HINCRBY", nil)

		srv.SetFailCmd("HGETALL", errors.New("hgetall err"))
		_, err = GetMessageReactionCounts(ctx, 1)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
	})
}
