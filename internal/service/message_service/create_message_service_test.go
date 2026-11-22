package message_service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateMessage_MediaWithHighIAScore_TriggersAutoReportWithoutBlockingMessage(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()
	defer time.Sleep(40 * time.Millisecond)

	senderID := int64(15)
	convID := int64(900)
	mediaID := int64(777)

	require.NoError(t, redis.UsersLite.SetObject(ctx, senderID, lite_models.UserLiteRequest{
		ID:         senderID,
		Username:   "sender_low_trust",
		TrustScore: 0.20,
	}))

	require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, conversation_models.ConversationPayload{
		ID:       convID,
		Type:     variables.ConversationTypeDirect,
		Settings: conversation_models.ConversationSettings{SendMediaPermission: true},
	}))
	require.NoError(t, object_cache_service.SetMemberInObjectCache(ctx, member_models.MemberPayload{
		ID:             1,
		ConversationID: convID,
		UserID:         senderID,
		Role:           variables.MemberRoleNormal,
	}))

	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, media_models.MediaPayload{
		ID:          mediaID,
		OwnerID:     senderID,
		StoragePath: "users/15/media/dm.avif",
		Visibility:  variables.MediaVisibilityPending,
		IAScore:     0.80,
	}))

	out, err := CreateMessage(ctx, senderID, convID, message_models.CreateMessageInput{
		ConversationID: convID,
		MessageType:    variables.MessageTypeMedia,
		Attachments: map[string]any{
			"media_id": mediaID,
		},
	}, false)
	require.NoError(t, err)
	assert.Greater(t, out.MessageID, int64(0))

	// 1. Le média du message est activé en Public (Visibility = 1, non bloqué en quarantaine)
	activatedMedia, errMed := object_cache_service.GetMediaFromObjectCache(ctx, mediaID)
	require.NoError(t, errMed)
	assert.Equal(t, variables.MediaVisibilityPublic, int(activatedMedia.Visibility))

	// 2. Un signalement automatique Robot (ReportOriginAutoAI = 1) a bien été créé pour ce message
	var agg moderation_service.TargetReportAggregation
	require.NoError(t, redis.ReportAggregators.GetObject(ctx, "3:"+strconv.FormatInt(out.MessageID, 10), &agg))
	assert.Equal(t, variables.ReportOriginAutoAI, agg.ReportOrigin)
	assert.Equal(t, 0.80, agg.IAScoreMax)
}

func TestCreateMessage_CommunityRecommendationTelemetryHook(t *testing.T) {
	test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()
	defer time.Sleep(40 * time.Millisecond)

	pubConv := conversation_models.ConversationPayload{
		ID:        7001,
		Type:      variables.ConversationTypeCommunityPub,
		Title:     "Public Community",
		State:     variables.ConversationStateActive,
		Lang:      "fr",
		Settings:  conversation_models.ConversationSettings{WritePermission: 0},
		CreatedAt: domain.NowMillis(),
		UpdatedAt: domain.NowMillis(),
	}
	groupConv := conversation_models.ConversationPayload{
		ID:        7002,
		Type:      variables.ConversationTypeGroup,
		Title:     "Private Group",
		State:     variables.ConversationStateActive,
		Settings:  conversation_models.DefaultConversationSettings(variables.ConversationTypeGroup),
		CreatedAt: domain.NowMillis(),
		UpdatedAt: domain.NowMillis(),
	}
	require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, pubConv))
	require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, groupConv))

	for _, uid := range []int64{401, 402} {
		for _, c := range []conversation_models.ConversationPayload{pubConv, groupConv} {
			m := member_models.MemberPayload{
				ID:             c.ID*10 + uid,
				ConversationID: c.ID,
				UserID:         uid,
				Role:           variables.MemberRoleNormal,
				Settings:       member_models.DefaultMemberSettings(c.Type),
				JoinedAt:       domain.NowMillis(),
				CreatedAt:      domain.NowMillis(),
				UpdatedAt:      domain.NowMillis(),
			}
			require.NoError(t, object_cache_service.SetMemberInObjectCache(ctx, m))
		}
	}

	_, err := CreateMessage(ctx, 401, pubConv.ID, message_models.CreateMessageInput{
		ConversationID: pubConv.ID,
		MessageType:    variables.MessageTypeText,
		Content:        "Bonjour communauté !",
	}, false)
	require.NoError(t, err)

	_, err = CreateMessage(ctx, 401, pubConv.ID, message_models.CreateMessageInput{
		ConversationID: pubConv.ID,
		MessageType:    variables.MessageTypeText,
		Content:        "Deuxième message du même auteur",
	}, false)
	require.NoError(t, err)

	_, err = CreateMessage(ctx, 402, pubConv.ID, message_models.CreateMessageInput{
		ConversationID: pubConv.ID,
		MessageType:    variables.MessageTypeText,
		Content:        "Salut de l'auteur 2",
	}, false)
	require.NoError(t, err)

	// Internal system message in public community must NOT increment telemetry
	_, err = CreateMessage(ctx, 401, pubConv.ID, message_models.CreateMessageInput{
		ConversationID: pubConv.ID,
		MessageType:    variables.MessageTypeSystem,
		Content:        "",
		Attachments:    map[string]any{"sys_action": variables.SysActionMemberJoined},
	}, true)
	require.NoError(t, err)

	// Message in standard group must NOT increment community telemetry
	_, err = CreateMessage(ctx, 401, groupConv.ID, message_models.CreateMessageInput{
		ConversationID: groupConv.ID,
		MessageType:    variables.MessageTypeText,
		Content:        "Message de groupe privé",
	}, false)
	require.NoError(t, err)

	dates := cache_service.CommunityRecoTrailingDates(domain.NowTime(), 7)
	msgCounts, err := cache_service.GetCommunity7DayMessageCounts(ctx, []int64{pubConv.ID, groupConv.ID}, dates)
	require.NoError(t, err)
	assert.Equal(t, int64(3), msgCounts[pubConv.ID])
	assert.Equal(t, int64(0), msgCounts[groupConv.ID])

	authorCounts, err := cache_service.GetCommunity7DayUniqueAuthors(ctx, []int64{pubConv.ID, groupConv.ID}, dates)
	require.NoError(t, err)
	assert.Equal(t, int64(2), authorCounts[pubConv.ID])
	assert.Equal(t, int64(0), authorCounts[groupConv.ID])
}
