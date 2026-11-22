package conversation_service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/conversation_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateCommunity(t *testing.T) {
	test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(101), lite_models.UserLiteRequest{
		ID:         101,
		Username:   "creator_mod",
		Grade:      variables.UserGradeModerator,
		TrustScore: 0.85,
	}))
	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(102), lite_models.UserLiteRequest{
		ID:         102,
		Username:   "designated_owner",
		Grade:      variables.UserGradeUser,
		TrustScore: 0.90,
	}))

	t.Run("creates community with normalized lang, ConvMeta, user:public_comms, and ranked score", func(t *testing.T) {
		out, err := conversation_service.CreateCommunity(ctx, 101, conversation_models.CreateCommunityInput{
			Title:   "Go Developers FR",
			Lang:    " FR ",
			OwnerID: 102,
			Settings: conversation_models.ConversationSettings{
				WritePermission: 2, // subreddit mode
			},
		})
		require.NoError(t, err)
		require.Greater(t, out.ConversationID, int64(0))

		convObj, errObj := object_cache_service.GetConversationFromObjectCache(ctx, out.ConversationID)
		require.NoError(t, errObj)
		assert.Equal(t, "fr", convObj.Lang)
		assert.False(t, convObj.IsPartner)
		assert.Equal(t, 0, convObj.PinSlot)

		var convMeta lite_models.ConvLiteRequest
		require.NoError(t, redis.ConvMeta.GetObject(ctx, out.ConversationID, &convMeta))
		assert.Equal(t, "fr", convMeta.Lang)
		assert.Equal(t, string(models.CommunityModeSubreddit), convMeta.Mode)

		ownerComms, errOwner := cache_service.GetUserPublicCommunities(ctx, 102)
		require.NoError(t, errOwner)
		assert.Contains(t, ownerComms, out.ConversationID)

		creatorComms, errCreator := cache_service.GetUserPublicCommunities(ctx, 101)
		require.NoError(t, errCreator)
		assert.Contains(t, creatorComms, out.ConversationID)

		score, found, errRank := cache_service.GetRankedCommunityScore(ctx, "fr", out.ConversationID)
		require.NoError(t, errRank)
		assert.True(t, found)
		assert.Greater(t, score, 0.0)
	})

	t.Run("rejects unsupported lang", func(t *testing.T) {
		_, err := conversation_service.CreateCommunity(ctx, 101, conversation_models.CreateCommunityInput{
			Title: "Invalid Lang",
			Lang:  "xx",
		})
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, 400, appErr.HTTPStatus)
	})
}
