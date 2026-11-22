package conversation_service_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/conversation_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateConversation_CommunityMetadataAndLangMigration(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()
	defer time.Sleep(40 * time.Millisecond)

	conv := conversation_models.ConversationPayload{
		ID:          5001,
		Type:        variables.ConversationTypeCommunityPub,
		Title:       "Tech FR",
		Description: "Initial desc",
		State:       variables.ConversationStateActive,
		Lang:        "fr",
		IsPartner:   true,
		PinSlot:     2,
		Settings:    conversation_models.DefaultConversationSettings(variables.ConversationTypeCommunityPub),
		CreatedAt:   domain.NowMillis(),
		UpdatedAt:   domain.NowMillis(),
	}
	owner := member_models.MemberPayload{
		ID:             9001,
		ConversationID: conv.ID,
		UserID:         201,
		Role:           variables.MemberRoleOwner,
		Settings:       member_models.DefaultMemberSettings(conv.Type),
		JoinedAt:       domain.NowMillis(),
		CreatedAt:      domain.NowMillis(),
		UpdatedAt:      domain.NowMillis(),
	}

	require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, conv))
	require.NoError(t, object_cache_service.SetMemberInObjectCache(ctx, owner))
	require.NoError(t, redis.ConvMeta.SetObject(ctx, conv.ID, service.ToConvLiteRequest(conv)))
	require.NoError(t, cache_service.UpsertRankedCommunityScore(ctx, "fr", conv.ID, 42.5))
	require.NoError(t, cache_service.SetCommunityPinInCache(ctx, "fr", 2, conv.ID))

	t.Run("partial update without Lang preserves Lang, IsPartner, PinSlot and updates Mode", func(t *testing.T) {
		newSettings := conv.Settings
		newSettings.WritePermission = 1 // announcement
		_, err := conversation_service.UpdateConversation(ctx, 201, conv.ID, conversation_models.UpdateConversationInput{
			ConversationID: conv.ID,
			Title:          "Tech FR Updated",
			Description:    "New desc",
			Settings:       newSettings,
		})
		require.NoError(t, err)

		updatedObj, errObj := object_cache_service.GetConversationFromObjectCache(ctx, conv.ID)
		require.NoError(t, errObj)
		assert.Equal(t, "fr", updatedObj.Lang)
		assert.True(t, updatedObj.IsPartner)
		assert.Equal(t, 2, updatedObj.PinSlot)

		var updatedMeta lite_models.ConvLiteRequest
		require.NoError(t, redis.ConvMeta.GetObject(ctx, conv.ID, &updatedMeta))
		assert.Equal(t, "fr", updatedMeta.Lang)
		assert.True(t, updatedMeta.IsPartner)
		assert.Equal(t, 2, updatedMeta.PinSlot)
		assert.Equal(t, string(models.CommunityModeAnnouncement), updatedMeta.Mode)
	})

	t.Run("changing Lang clears pin in old lang, updates Postgres, and migrates ranked score", func(t *testing.T) {
		pgCalled := false
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "func_update_community_lang") {
				pgCalled = true
				return test.NewMockRows(
					[]string{"status_code", "old_lang", "new_lang", "old_pin_slot"},
					[][]driver.Value{{"OK", "fr", "en", int64(2)}},
				), nil
			}
			return test.NewMockRows([]string{"id"}, nil), nil
		}, nil)

		_, err := conversation_service.UpdateConversation(ctx, 201, conv.ID, conversation_models.UpdateConversationInput{
			ConversationID: conv.ID,
			Title:          "Tech EN",
			Description:    "Now English",
			Settings:       conv.Settings,
			Lang:           "EN",
		})
		require.NoError(t, err)
		assert.True(t, pgCalled, "FuncUpdateCommunityLang must be called when Postgres is initialized")

		updatedObj, errObj := object_cache_service.GetConversationFromObjectCache(ctx, conv.ID)
		require.NoError(t, errObj)
		assert.Equal(t, "en", updatedObj.Lang)
		assert.Equal(t, 0, updatedObj.PinSlot)
		assert.True(t, updatedObj.IsPartner)

		var updatedMeta lite_models.ConvLiteRequest
		require.NoError(t, redis.ConvMeta.GetObject(ctx, conv.ID, &updatedMeta))
		assert.Equal(t, "en", updatedMeta.Lang)
		assert.Equal(t, 0, updatedMeta.PinSlot)

		frPins, errPins := cache_service.GetCommunityPinsByLang(ctx, "fr")
		require.NoError(t, errPins)
		assert.Empty(t, frPins)

		_, foundFR, errFR := cache_service.GetRankedCommunityScore(ctx, "fr", conv.ID)
		require.NoError(t, errFR)
		assert.False(t, foundFR)

		scoreEN, foundEN, errEN := cache_service.GetRankedCommunityScore(ctx, "en", conv.ID)
		require.NoError(t, errEN)
		assert.True(t, foundEN)
		assert.InDelta(t, 42.5, scoreEN, 1e-6)
	})

	t.Run("rejects Lang on non-public group with 403 Forbidden", func(t *testing.T) {
		group := conversation_models.ConversationPayload{
			ID:        5002,
			Type:      variables.ConversationTypeGroup,
			Title:     "Friends Group",
			State:     variables.ConversationStateActive,
			Settings:  conversation_models.DefaultConversationSettings(variables.ConversationTypeGroup),
			CreatedAt: domain.NowMillis(),
			UpdatedAt: domain.NowMillis(),
		}
		groupOwner := member_models.MemberPayload{
			ID:             9002,
			ConversationID: group.ID,
			UserID:         201,
			Role:           variables.MemberRoleOwner,
			Settings:       member_models.DefaultMemberSettings(group.Type),
			JoinedAt:       domain.NowMillis(),
			CreatedAt:      domain.NowMillis(),
			UpdatedAt:      domain.NowMillis(),
		}
		require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, group))
		require.NoError(t, object_cache_service.SetMemberInObjectCache(ctx, groupOwner))

		_, err := conversation_service.UpdateConversation(ctx, 201, group.ID, conversation_models.UpdateConversationInput{
			ConversationID: group.ID,
			Title:          "Friends Group",
			Settings:       group.Settings,
			Lang:           "fr",
		})
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, 403, appErr.HTTPStatus)
	})
}
