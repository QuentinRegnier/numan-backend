package cache_service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpeedCacheCommunitiesService(t *testing.T) {
	ctx := context.Background()

	t.Run("StoreCommunityLiteInSpeedCache and SearchCommunitiesByPrefix", func(t *testing.T) {
		_ = test.SetupTestRedis(t)

		c1 := lite_models.CommunityLiteRequest{ID: 10, Name: "Golang Devs", MemberCount: 5}
		c2 := lite_models.CommunityLiteRequest{ID: 20, Name: "Go France", MemberCount: 12}
		require.NoError(t, StoreCommunityLiteInSpeedCache(ctx, c1))
		require.NoError(t, StoreCommunityLiteInSpeedCache(ctx, c2))

		results, err := SearchCommunitiesByPrefix(ctx, "go", 0, 10)
		require.NoError(t, err)
		require.Len(t, results, 2)
		assert.Equal(t, int64(20), results[0].ID) // "go france:20" < "golang devs:10"
		assert.Equal(t, int64(10), results[1].ID)

		emptyRes, err := SearchCommunitiesByPrefix(ctx, "rust", 0, 10)
		require.NoError(t, err)
		assert.Empty(t, emptyRes)
	})

	t.Run("updateCommunityMemberCountInSpeedCache", func(t *testing.T) {
		srv := test.SetupTestRedis(t)

		c := lite_models.CommunityLiteRequest{ID: 50, Name: "Numan", MemberCount: 2}
		require.NoError(t, StoreCommunityLiteInSpeedCache(ctx, c))

		// delta 0 is no-op
		updateCommunityMemberCountInSpeedCache(ctx, 50, 0)

		// increment
		updateCommunityMemberCountInSpeedCache(ctx, 50, 3)
		var loaded lite_models.CommunityLiteRequest
		require.NoError(t, redis.SpeedCommunity.GetObject(ctx, 50, &loaded))
		assert.Equal(t, 5, loaded.MemberCount)

		// clamp negative to 0
		updateCommunityMemberCountInSpeedCache(ctx, 50, -10)
		require.NoError(t, redis.SpeedCommunity.GetObject(ctx, 50, &loaded))
		assert.Equal(t, 0, loaded.MemberCount)

		// SET error logged gracefully
		srv.SetFailCmd("SET", errors.New("set err"))
		updateCommunityMemberCountInSpeedCache(ctx, 50, 1)
		srv.SetFailCmd("SET", nil)

		// GET error logged gracefully
		srv.SetFailCmd("GET", errors.New("get err"))
		updateCommunityMemberCountInSpeedCache(ctx, 50, 1)
		srv.SetFailCmd("GET", nil)

		// Non-existent community is ignored silently
		updateCommunityMemberCountInSpeedCache(ctx, 9999, 1)
	})

	t.Run("Error Paths", func(t *testing.T) {
		srv := test.SetupTestRedis(t)

		c := lite_models.CommunityLiteRequest{ID: 77, Name: "Test"}
		srv.SetFailCmd("ZADD", errors.New("zadd err"))
		err := StoreCommunityLiteInSpeedCache(ctx, c)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusInternalServerError, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)

		srv.SetFailCmd("ZADD", nil)
		srv.SetFailCmd("SET", errors.New("set err"))
		err = StoreCommunityLiteInSpeedCache(ctx, c)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("SET", nil)

		srv.SetFailCmd("ZRANGEBYLEX", errors.New("lex err"))
		_, err = SearchCommunitiesByPrefix(ctx, "te", 0, 10)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("ZRANGEBYLEX", nil)

		require.NoError(t, redis.CommunitiesLex.ZAdd(ctx, variables.LexicographicGlobalKey, 0, "test:77"))
		srv.SetFailCmd("MGET", errors.New("mget err"))
		_, err = SearchCommunitiesByPrefix(ctx, "te", 0, 10)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("MGET", nil)

		// Corrupted msgpack payload
		require.NoError(t, redis.SpeedCommunity.SetPrimitive(ctx, 77, "corrupted-msgpack"))
		res, err := SearchCommunitiesByPrefix(ctx, "te", 0, 10)
		require.NoError(t, err)
		assert.Empty(t, res)
	})
}
