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

func TestBanMember_RemovesUserFromPublicCommunitiesIndex(t *testing.T) {
	test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()
	defer time.Sleep(40 * time.Millisecond)

	pubConv := conversation_models.ConversationPayload{
		ID:        8101,
		Type:      variables.ConversationTypeCommunityPub,
		Title:     "Public Comm",
		State:     variables.ConversationStateActive,
		Lang:      "fr",
		Settings:  conversation_models.DefaultConversationSettings(variables.ConversationTypeCommunityPub),
		CreatedAt: domain.NowMillis(),
		UpdatedAt: domain.NowMillis(),
	}
	require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, pubConv))
	require.NoError(t, object_cache_service.SetMemberInObjectCache(ctx, member_models.MemberPayload{
		ID:             81011,
		ConversationID: pubConv.ID,
		UserID:         501,
		Role:           variables.MemberRoleOwner,
		Settings:       member_models.DefaultMemberSettings(pubConv.Type),
		JoinedAt:       domain.NowMillis(),
		CreatedAt:      domain.NowMillis(),
		UpdatedAt:      domain.NowMillis(),
	}))
	require.NoError(t, object_cache_service.SetMemberInObjectCache(ctx, member_models.MemberPayload{
		ID:             81012,
		ConversationID: pubConv.ID,
		UserID:         502,
		Role:           variables.MemberRoleNormal,
		Settings:       member_models.DefaultMemberSettings(pubConv.Type),
		JoinedAt:       domain.NowMillis(),
		CreatedAt:      domain.NowMillis(),
		UpdatedAt:      domain.NowMillis(),
	}))
	_, err := cache_service.AddUserPublicCommunity(ctx, 502, pubConv.ID)
	require.NoError(t, err)

	_, err = member_service.BanMember(ctx, 501, member_models.BanMemberInput{
		ConversationID: pubConv.ID,
		TargetUserID:   502,
	})
	require.NoError(t, err)

	commsAfterBan, err := cache_service.GetUserPublicCommunities(ctx, 502)
	require.NoError(t, err)
	assert.NotContains(t, commsAfterBan, pubConv.ID)
}
