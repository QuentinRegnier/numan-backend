package community_recommendation_service

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveTargetCommunityLang(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()

	// 1. Explicit lang always wins and is normalized
	assert.Equal(t, "en", resolveTargetCommunityLang(ctx, 10, "EN"))

	// 2. Falls back to UserSettings language when explicit lang is empty
	settings := user_settings_models.UserSettingsPayload{
		ID:     20,
		UserID: 20,
		DisplayAndContent: user_settings_models.DisplayAndContentSettings{
			Language: variables.LangES,
		},
	}
	require.NoError(t, object_cache_service.SetUserSettings(ctx, settings))
	assert.Equal(t, "es", resolveTargetCommunityLang(ctx, 20, ""))

	// 3. Falls back to platform default ("fr") when neither explicit nor settings exist
	assert.Equal(t, variables.DefaultLanguageISO, resolveTargetCommunityLang(ctx, 99999, ""))
}

func TestLoadConvMetasWithFallback_HealsFromObjectCache(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()

	activePub := conversation_models.ConversationPayload{
		ID:          501,
		Type:        variables.ConversationTypeCommunityPub,
		State:       variables.ConversationStateActive,
		Title:       "Healed Community",
		Description: "From object cache",
		Lang:        "fr",
		IsPartner:   true,
		PinSlot:     2,
	}
	require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, activePub))

	// Inactive community must not be hydrated
	inactivePub := conversation_models.ConversationPayload{
		ID:    502,
		Type:  variables.ConversationTypeCommunityPub,
		State: variables.ConversationStateArchived,
		Title: "Archived",
		Lang:  "fr",
	}
	require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, inactivePub))

	metas := loadConvMetasWithFallback(ctx, []int64{501, 502})
	require.Contains(t, metas, int64(501))
	assert.NotContains(t, metas, int64(502))
	assert.Equal(t, "Healed Community", metas[501].Title)
	assert.Equal(t, 2, metas[501].PinSlot)

	// ConvMeta should now be healed in Redis L1
	var cached lite_models.ConvLiteRequest
	require.NoError(t, redis.ConvMeta.GetObject(ctx, int64(501), &cached))
	assert.Equal(t, "Healed Community", cached.Title)
	assert.Equal(t, 2, cached.PinSlot)
}

func TestSyncCommunityCachesAfterAdminMutation(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()

	t.Run("updates_object_cache_and_conv_meta_when_object_present", func(t *testing.T) {
		orig := conversation_models.ConversationPayload{
			ID:        601,
			Type:      variables.ConversationTypeCommunityPub,
			State:     variables.ConversationStateActive,
			Title:     "Sync Test",
			Lang:      "fr",
			IsPartner: false,
			PinSlot:   0,
		}
		require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, orig))

		syncCommunityCachesAfterAdminMutation(ctx, 601, func(c conversation_models.ConversationPayload) conversation_models.ConversationPayload {
			c.IsPartner = true
			c.PinSlot = 3
			return c
		}, func(m lite_models.ConvLiteRequest) lite_models.ConvLiteRequest {
			m.IsPartner = true
			m.PinSlot = 3
			return m
		})

		updatedObj, err := object_cache_service.GetConversationFromObjectCache(ctx, 601)
		require.NoError(t, err)
		assert.True(t, updatedObj.IsPartner)
		assert.Equal(t, 3, updatedObj.PinSlot)

		var updatedMeta lite_models.ConvLiteRequest
		require.NoError(t, redis.ConvMeta.GetObject(ctx, int64(601), &updatedMeta))
		assert.True(t, updatedMeta.IsPartner)
		assert.Equal(t, 3, updatedMeta.PinSlot)
	})

	t.Run("updates_conv_meta_in_place_when_object_cache_misses", func(t *testing.T) {
		require.NoError(t, redis.ConvMeta.SetObject(ctx, int64(602), lite_models.ConvLiteRequest{
			ID:        602,
			Type:      variables.ConversationTypeCommunityPub,
			Title:     "Meta Only",
			Lang:      "fr",
			IsPartner: false,
		}))

		syncCommunityCachesAfterAdminMutation(ctx, 602, func(c conversation_models.ConversationPayload) conversation_models.ConversationPayload {
			c.IsPartner = true
			return c
		}, func(m lite_models.ConvLiteRequest) lite_models.ConvLiteRequest {
			m.IsPartner = true
			return m
		})

		var updatedMeta lite_models.ConvLiteRequest
		require.NoError(t, redis.ConvMeta.GetObject(ctx, int64(602), &updatedMeta))
		assert.True(t, updatedMeta.IsPartner)
	})
}
