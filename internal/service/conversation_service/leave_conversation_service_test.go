package conversation_service_test

import (
	"context"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
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

func TestLeaveConversation_PublicCommunityMembershipAndExtinctionCleanup(t *testing.T) {
	test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()
	defer time.Sleep(40 * time.Millisecond)

	conv := conversation_models.ConversationPayload{
		ID:        6301,
		Type:      variables.ConversationTypeCommunityPub,
		Title:     "Closing Community",
		State:     variables.ConversationStateActive,
		Lang:      "fr",
		PinSlot:   1,
		Settings:  conversation_models.DefaultConversationSettings(variables.ConversationTypeCommunityPub),
		CreatedAt: domain.NowMillis(),
		UpdatedAt: domain.NowMillis(),
	}
	owner := member_models.MemberPayload{
		ID:             9301,
		ConversationID: conv.ID,
		UserID:         301,
		Role:           variables.MemberRoleOwner,
		Settings:       member_models.DefaultMemberSettings(conv.Type),
		JoinedAt:       domain.NowMillis(),
		CreatedAt:      domain.NowMillis(),
		UpdatedAt:      domain.NowMillis(),
	}
	member := member_models.MemberPayload{
		ID:             9302,
		ConversationID: conv.ID,
		UserID:         302,
		Role:           variables.MemberRoleNormal,
		Settings:       member_models.DefaultMemberSettings(conv.Type),
		JoinedAt:       domain.NowMillis(),
		CreatedAt:      domain.NowMillis(),
		UpdatedAt:      domain.NowMillis(),
	}
	require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, conv))
	require.NoError(t, object_cache_service.SetMemberInObjectCache(ctx, owner))
	require.NoError(t, object_cache_service.SetMemberInObjectCache(ctx, member))
	require.NoError(t, redis.ConvParticipants.SAdd(ctx, conv.ID, owner.UserID, member.UserID))
	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(301), lite_models.UserLiteRequest{ID: 301, Username: "owner"}))
	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(302), lite_models.UserLiteRequest{ID: 302, Username: "member"}))

	_, err := cache_service.AddUserPublicCommunity(ctx, owner.UserID, conv.ID)
	require.NoError(t, err)
	_, err = cache_service.AddUserPublicCommunity(ctx, member.UserID, conv.ID)
	require.NoError(t, err)
	require.NoError(t, cache_service.UpsertRankedCommunityScore(ctx, "fr", conv.ID, 15.0))
	require.NoError(t, cache_service.SetCommunityPinInCache(ctx, "fr", 1, conv.ID))

	// Member 302 leaves -> removed from user:public_comms; community remains active
	_, err = conversation_service.LeaveConversation(ctx, 302, conversation_models.LeaveConversationInput{ConversationID: conv.ID})
	require.NoError(t, err)
	comms302, err := cache_service.GetUserPublicCommunities(ctx, 302)
	require.NoError(t, err)
	assert.NotContains(t, comms302, conv.ID)

	// Last participant (owner 301) leaves -> extinction cleans ranked ZSET + pins + PinSlot
	_, err = conversation_service.LeaveConversation(ctx, 301, conversation_models.LeaveConversationInput{ConversationID: conv.ID})
	require.NoError(t, err)

	comms301, err := cache_service.GetUserPublicCommunities(ctx, 301)
	require.NoError(t, err)
	assert.NotContains(t, comms301, conv.ID)

	_, foundRanked, err := cache_service.GetRankedCommunityScore(ctx, "fr", conv.ID)
	require.NoError(t, err)
	assert.False(t, foundRanked)

	pinsAfter, err := cache_service.GetCommunityPinsByLang(ctx, "fr")
	require.NoError(t, err)
	assert.Empty(t, pinsAfter)

	convObj, err := object_cache_service.GetConversationFromObjectCache(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, convObj.PinSlot)
}
