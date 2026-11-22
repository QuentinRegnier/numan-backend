package community_recommendation_service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/community_recommendation_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedTestCommunity(t *testing.T, ctx context.Context, id int64, title, lang string, writePerm int, isPartner bool, memberCount int) {
	t.Helper()
	conv := conversation_models.ConversationPayload{
		ID:          id,
		Type:        variables.ConversationTypeCommunityPub,
		Title:       title,
		Description: "Description of " + title,
		AvatarID:    id * 10,
		State:       variables.ConversationStateActive,
		Lang:        lang,
		IsPartner:   isPartner,
		Settings: conversation_models.ConversationSettings{
			WritePermission: writePerm,
		},
	}
	require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, conv))
	require.NoError(t, redis.ConvMeta.SetObject(ctx, id, service.ToConvLiteRequest(conv)))
	require.NoError(t, redis.SpeedCommunity.SetObject(ctx, id, lite_models.CommunityLiteRequest{
		ID:          id,
		Name:        title,
		MemberCount: memberCount,
	}))
}

func TestGetCommunityRecommendations_FullFlowFriendBoostPinsAndNoIsJoined(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()

	// Caller 42 prefers English in UserSettings (LangEN = 1 -> "en")
	require.NoError(t, redis.UserSettings.SetObject(ctx, int64(42), user_settings_models.UserSettingsPayload{
		ID: 42,
		DisplayAndContent: user_settings_models.DisplayAndContentSettings{
			Language: variables.LangEN,
		},
	}))

	seedTestCommunity(t, ctx, 101, "Alpha EN", "en", 0, false, 100)
	seedTestCommunity(t, ctx, 102, "Beta EN", "en", 2, true, 50)
	seedTestCommunity(t, ctx, 103, "Gamma EN", "en", 1, false, 30)
	seedTestCommunity(t, ctx, 201, "French Only", "fr", 0, false, 500)

	// Ranked global scores in "en": 101 (10.0) > 102 (8.0) > 103 (5.0)
	require.NoError(t, cache_service.ReplaceRankedCommunitiesByLang(ctx, "en", []cache_service.RankedCommunityEntry{
		{ConversationID: 101, Score: 10.0},
		{ConversationID: 102, Score: 8.0},
		{ConversationID: 103, Score: 5.0},
	}))
	require.NoError(t, cache_service.ReplaceRankedCommunitiesByLang(ctx, "fr", []cache_service.RankedCommunityEntry{
		{ConversationID: 201, Score: 99.0},
	}))

	// Caller 42 has 5 direct friends, all members of community 102 -> boosts 102 above 101
	for i := int64(1); i <= 5; i++ {
		fid := 500 + i
		require.NoError(t, redis.SpeedRelationsIndex.ZAdd(ctx, fmt.Sprintf("in:%d:%d", variables.RelationStateFriend, 42), float64(i), fmt.Sprintf("%d", fid)))
		_, err := cache_service.AddUserPublicCommunity(ctx, fid, 102)
		require.NoError(t, err)
	}

	// Admin pins community 103 at exact slot 1 for "en"
	require.NoError(t, cache_service.SetCommunityPinInCache(ctx, "en", 1, 103))

	// Omit explicit lang -> should resolve "en" from UserSettings
	out, err := community_recommendation_service.GetCommunityRecommendations(ctx, 42, community_recommendation_models.GetCommunityRecommendationsInput{
		Limit:  20,
		Offset: 0,
	})
	require.NoError(t, err)

	assert.Equal(t, "en", out.Lang)
	assert.Equal(t, 3, out.Total)
	assert.False(t, out.HasMore)
	require.Len(t, out.Communities, 3)

	// Position 1: pinned 103
	assert.Equal(t, int64(103), out.Communities[0].ID)
	assert.True(t, out.Communities[0].IsPinned)
	assert.Equal(t, 1, out.Communities[0].PinSlot)
	assert.Equal(t, "announcement", out.Communities[0].Mode)

	// Position 2: friend-boosted 102
	assert.Equal(t, int64(102), out.Communities[1].ID)
	assert.False(t, out.Communities[1].IsPinned)
	assert.Equal(t, 0, out.Communities[1].PinSlot)
	assert.Equal(t, 5, out.Communities[1].FriendsCount)
	assert.True(t, out.Communities[1].IsPartner)
	assert.Equal(t, "subreddit", out.Communities[1].Mode)
	assert.Equal(t, 50, out.Communities[1].MemberCount)

	// Position 3: organic 101
	assert.Equal(t, int64(101), out.Communities[2].ID)
	assert.Equal(t, 0, out.Communities[2].FriendsCount)
	assert.Equal(t, "free_chat", out.Communities[2].Mode)

	// Strict JSON contract check: NO is_joined field anywhere in serialized JSON
	rawJSON, err := json.Marshal(out)
	require.NoError(t, err)
	assert.NotContains(t, string(rawJSON), "is_joined")

	// Explicit lang override "fr" ignores UserSettings
	outFR, err := community_recommendation_service.GetCommunityRecommendations(ctx, 42, community_recommendation_models.GetCommunityRecommendationsInput{
		Lang: "FR",
	})
	require.NoError(t, err)
	assert.Equal(t, "fr", outFR.Lang)
	require.Len(t, outFR.Communities, 1)
	assert.Equal(t, int64(201), outFR.Communities[0].ID)

	// Pagination across pinned boundary
	page1, err := community_recommendation_service.GetCommunityRecommendations(ctx, 42, community_recommendation_models.GetCommunityRecommendationsInput{
		Lang:   "en",
		Limit:  2,
		Offset: 0,
	})
	require.NoError(t, err)
	assert.True(t, page1.HasMore)
	require.Len(t, page1.Communities, 2)
	assert.Equal(t, int64(103), page1.Communities[0].ID)
	assert.Equal(t, int64(102), page1.Communities[1].ID)

	page2, err := community_recommendation_service.GetCommunityRecommendations(ctx, 42, community_recommendation_models.GetCommunityRecommendationsInput{
		Lang:   "en",
		Limit:  2,
		Offset: 2,
	})
	require.NoError(t, err)
	assert.False(t, page2.HasMore)
	require.Len(t, page2.Communities, 1)
	assert.Equal(t, int64(101), page2.Communities[0].ID)
}

func TestGetCommunityRecommendations_DefaultLangFallbackAndValidation(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()

	out, err := community_recommendation_service.GetCommunityRecommendations(ctx, 999, community_recommendation_models.GetCommunityRecommendationsInput{})
	require.NoError(t, err)
	assert.Equal(t, variables.DefaultLanguageISO, out.Lang)
	assert.Empty(t, out.Communities)
	assert.NotNil(t, out.Communities)

	var appErr *numan_error.AppError
	_, err = community_recommendation_service.GetCommunityRecommendations(ctx, 0, community_recommendation_models.GetCommunityRecommendationsInput{})
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)

	_, err = community_recommendation_service.GetCommunityRecommendations(ctx, 42, community_recommendation_models.GetCommunityRecommendationsInput{Lang: "xx"})
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
}
