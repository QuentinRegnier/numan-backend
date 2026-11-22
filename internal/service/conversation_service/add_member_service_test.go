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

func TestAddMembersToConversation_PublicCommunityHooksIdempotent(t *testing.T) {
	test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()
	defer time.Sleep(40 * time.Millisecond)

	conv := conversation_models.ConversationPayload{
		ID:        6201,
		Type:      variables.ConversationTypeCommunityPub,
		Title:     "Open Community",
		State:     variables.ConversationStateActive,
		Lang:      "fr",
		Settings:  conversation_models.ConversationSettings{AddMemberPermission: true},
		CreatedAt: domain.NowMillis(),
		UpdatedAt: domain.NowMillis(),
	}
	owner := member_models.MemberPayload{
		ID:             9201,
		ConversationID: conv.ID,
		UserID:         301,
		Role:           variables.MemberRoleOwner,
		Settings:       member_models.DefaultMemberSettings(conv.Type),
		JoinedAt:       domain.NowMillis(),
		CreatedAt:      domain.NowMillis(),
		UpdatedAt:      domain.NowMillis(),
	}
	require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, conv))
	require.NoError(t, object_cache_service.SetMemberInObjectCache(ctx, owner))
	require.NoError(t, redis.ConvParticipants.SAdd(ctx, conv.ID, owner.UserID))
	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(301), lite_models.UserLiteRequest{ID: 301, Username: "owner"}))
	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(303), lite_models.UserLiteRequest{ID: 303, Username: "added_user", ConversationPermission: 0, AddGroupPermission: 0}))

	dates := cache_service.CommunityRecoTrailingDates(domain.NowTime(), 7)

	_, err := conversation_service.AddMembersToConversation(ctx, 301, conversation_models.AddMemberInput{
		ConversationID: conv.ID,
		ParticipantIDs: []int64{303},
	})
	require.NoError(t, err)

	comms, err := cache_service.GetUserPublicCommunities(ctx, 303)
	require.NoError(t, err)
	assert.Contains(t, comms, conv.ID)

	growthMap, err := cache_service.GetCommunity7DayGrowthCounts(ctx, []int64{conv.ID}, dates)
	require.NoError(t, err)
	assert.Equal(t, int64(1), growthMap[conv.ID])

	// Duplicate add must not inflate 7d growth
	_, err = conversation_service.AddMembersToConversation(ctx, 301, conversation_models.AddMemberInput{
		ConversationID: conv.ID,
		ParticipantIDs: []int64{303},
	})
	require.NoError(t, err)

	growthMap, err = cache_service.GetCommunity7DayGrowthCounts(ctx, []int64{conv.ID}, dates)
	require.NoError(t, err)
	assert.Equal(t, int64(1), growthMap[conv.ID])
}
