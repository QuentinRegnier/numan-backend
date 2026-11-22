package member_service_test

import (
	"context"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/member_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAcceptCommunityRequest_PublicVsPrivateCommunityHooks(t *testing.T) {
	test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()
	defer time.Sleep(40 * time.Millisecond)

	pubConv := conversation_models.ConversationPayload{
		ID:        8001,
		Type:      variables.ConversationTypeCommunityPub,
		Title:     "Public Comm",
		State:     variables.ConversationStateActive,
		Lang:      "fr",
		Settings:  conversation_models.DefaultConversationSettings(variables.ConversationTypeCommunityPub),
		CreatedAt: domain.NowMillis(),
		UpdatedAt: domain.NowMillis(),
	}
	privConv := conversation_models.ConversationPayload{
		ID:        8002,
		Type:      variables.ConversationTypeCommunityPriv,
		Title:     "Private Comm",
		State:     variables.ConversationStateActive,
		Settings:  conversation_models.DefaultConversationSettings(variables.ConversationTypeCommunityPriv),
		CreatedAt: domain.NowMillis(),
		UpdatedAt: domain.NowMillis(),
	}
	require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, pubConv))
	require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, privConv))

	for _, c := range []conversation_models.ConversationPayload{pubConv, privConv} {
		require.NoError(t, object_cache_service.SetMemberInObjectCache(ctx, member_models.MemberPayload{
			ID:             c.ID*10 + 1,
			ConversationID: c.ID,
			UserID:         501,
			Role:           variables.MemberRoleOwner,
			Settings:       member_models.DefaultMemberSettings(c.Type),
			JoinedAt:       domain.NowMillis(),
			CreatedAt:      domain.NowMillis(),
			UpdatedAt:      domain.NowMillis(),
		}))
		require.NoError(t, object_cache_service.SetMemberInObjectCache(ctx, member_models.MemberPayload{
			ID:             c.ID*10 + 2,
			ConversationID: c.ID,
			UserID:         502,
			Role:           variables.MemberRolePending,
			Settings:       member_models.DefaultMemberSettings(c.Type),
			JoinedAt:       domain.NowMillis(),
			CreatedAt:      domain.NowMillis(),
			UpdatedAt:      domain.NowMillis(),
		}))
	}

	_, err := member_service.AcceptCommunityRequest(ctx, 501, member_models.AcceptCommunityRequestInput{
		ConversationID: pubConv.ID,
		TargetUserID:   502,
	})
	require.NoError(t, err)

	_, err = member_service.AcceptCommunityRequest(ctx, 501, member_models.AcceptCommunityRequestInput{
		ConversationID: privConv.ID,
		TargetUserID:   502,
	})
	require.NoError(t, err)

	comms, err := cache_service.GetUserPublicCommunities(ctx, 502)
	require.NoError(t, err)
	assert.Contains(t, comms, pubConv.ID)
	assert.NotContains(t, comms, privConv.ID)

	dates := cache_service.CommunityRecoTrailingDates(domain.NowTime(), 7)
	growthMap, err := cache_service.GetCommunity7DayGrowthCounts(ctx, []int64{pubConv.ID, privConv.ID}, dates)
	require.NoError(t, err)
	assert.Equal(t, int64(1), growthMap[pubConv.ID])
	assert.Equal(t, int64(0), growthMap[privConv.ID])
}
