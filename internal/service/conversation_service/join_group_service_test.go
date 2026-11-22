package conversation_service_test

import (
	"context"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
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

func TestJoinGroup_PublicCommunityHooks(t *testing.T) {
	test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()
	defer time.Sleep(40 * time.Millisecond)

	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(302), lite_models.UserLiteRequest{ID: 302, Username: "joiner"}))
	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(304), lite_models.UserLiteRequest{ID: 304, Username: "pending_user"}))

	dates := cache_service.CommunityRecoTrailingDates(domain.NowTime(), 7)

	t.Run("direct join adds user:public_comms and increments 7d growth", func(t *testing.T) {
		openConv := conversation_models.ConversationPayload{
			ID:        6101,
			Type:      variables.ConversationTypeCommunityPub,
			Title:     "Open Community",
			State:     variables.ConversationStateActive,
			Lang:      "fr",
			Settings:  conversation_models.ConversationSettings{AddMemberPermission: true},
			CreatedAt: domain.NowMillis(),
			UpdatedAt: domain.NowMillis(),
		}
		require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, openConv))

		_, err := conversation_service.JoinGroup(ctx, 302, conversation_models.JoinGroupInput{
			ConversationID: openConv.ID,
			Internal:       true,
		})
		require.NoError(t, err)

		comms, err := cache_service.GetUserPublicCommunities(ctx, 302)
		require.NoError(t, err)
		assert.Contains(t, comms, openConv.ID)

		growthMap, err := cache_service.GetCommunity7DayGrowthCounts(ctx, []int64{openConv.ID}, dates)
		require.NoError(t, err)
		assert.Equal(t, int64(1), growthMap[openConv.ID])
	})

	t.Run("pending join request does not add user:public_comms or growth", func(t *testing.T) {
		approvalConv := conversation_models.ConversationPayload{
			ID:        6102,
			Type:      variables.ConversationTypeCommunityPub,
			Title:     "Approval Community",
			State:     variables.ConversationStateActive,
			Lang:      "fr",
			Settings:  conversation_models.ConversationSettings{JoinApprovalRequired: true},
			CreatedAt: domain.NowMillis(),
			UpdatedAt: domain.NowMillis(),
		}
		require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, approvalConv))

		_, err := conversation_service.JoinGroup(ctx, 304, conversation_models.JoinGroupInput{
			ConversationID: approvalConv.ID,
			External:       true,
		})
		require.NoError(t, err)

		comms, err := cache_service.GetUserPublicCommunities(ctx, 304)
		require.NoError(t, err)
		assert.NotContains(t, comms, approvalConv.ID)

		growthMap, err := cache_service.GetCommunity7DayGrowthCounts(ctx, []int64{approvalConv.ID}, dates)
		require.NoError(t, err)
		assert.Equal(t, int64(0), growthMap[approvalConv.ID])
	})
}
