package community_recommendation_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
)

// PinCommunity épingle atomiquement une communauté publique active sur un slot 1..100
// pour sa langue, avec détection de collision 409 ou remplacement forcé.
func PinCommunity(
	ctx context.Context,
	adminID int64,
	input community_recommendation_models.PinCommunityInput,
) (community_recommendation_models.PinCommunityOutput, error) {
	if err := security_service.LeftAdmin(ctx, adminID); err != nil {
		return community_recommendation_models.PinCommunityOutput{}, err
	}
	if err := input.Validate(); err != nil {
		return community_recommendation_models.PinCommunityOutput{}, err
	}

	res, err := postgres.FuncSetCommunityPin(ctx, input.ConversationID, input.Slot, input.ForceReplace)
	if err != nil {
		return community_recommendation_models.PinCommunityOutput{}, err
	}

	switch res.StatusCode {
	case "OK":
		// Continue
	case "NOT_FOUND":
		return community_recommendation_models.PinCommunityOutput{}, numan_error.NewNotFound(
			numan_error.CodeNotFound,
			"Communauté introuvable.",
			nil,
		)
	case "NOT_ACTIVE_PUBLIC_COMMUNITY":
		return community_recommendation_models.PinCommunityOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Seule une communauté publique active peut être épinglée.",
			nil,
		)
	case "MISSING_LANG":
		return community_recommendation_models.PinCommunityOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"La communauté doit posséder une langue valide avant d'être épinglée.",
			nil,
		)
	case "INVALID_SLOT":
		return community_recommendation_models.PinCommunityOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Le slot d'épinglage doit être compris entre 1 et 100.",
			nil,
		)
	case "CONFLICT":
		return community_recommendation_models.PinCommunityOutput{}, numan_error.NewConflict(
			numan_error.CodeConflict,
			"Ce slot d'épinglage est déjà occupé pour cette langue. Utilisez force_replace=true pour remplacer l'épinglage existant.",
			nil,
		)
	default:
		return community_recommendation_models.PinCommunityOutput{}, numan_error.NewInternal()
	}

	// Synchronisation immédiate du HASH Redis pins
	if res.OldPinSlot > 0 && res.OldPinSlot != input.Slot {
		if errDel := cache_service.DeleteCommunityPinInCache(ctx, res.ConvLang, res.OldPinSlot); errDel != nil {
			numan_log.Warn(ctx).Err(errDel).Int64("conversation_id", input.ConversationID).Int("old_slot", res.OldPinSlot).Msg("Échec suppression ancien slot pin en cache L1")
		}
	}
	if errSet := cache_service.SetCommunityPinInCache(ctx, res.ConvLang, input.Slot, input.ConversationID); errSet != nil {
		numan_log.Warn(ctx).Err(errSet).Int64("conversation_id", input.ConversationID).Int("slot", input.Slot).Msg("Échec écriture slot pin en cache L1")
	}

	// Synchronisation L1 Object Cache, ConvMeta & L2 MongoDB (cible + éventuellement déplacée)
	syncCommunityCachesAfterAdminMutation(ctx, input.ConversationID, func(c conversation_models.ConversationPayload) conversation_models.ConversationPayload {
		c.PinSlot = input.Slot
		return c
	}, func(m lite_models.ConvLiteRequest) lite_models.ConvLiteRequest {
		m.PinSlot = input.Slot
		return m
	})

	if res.DisplacedConversationID > 0 {
		syncCommunityCachesAfterAdminMutation(ctx, res.DisplacedConversationID, func(c conversation_models.ConversationPayload) conversation_models.ConversationPayload {
			c.PinSlot = 0
			return c
		}, func(m lite_models.ConvLiteRequest) lite_models.ConvLiteRequest {
			m.PinSlot = 0
			return m
		})
	}

	logEvt := numan_log.Info(ctx).
		Int64("admin_id", adminID).
		Int64("conversation_id", input.ConversationID).
		Str("lang", res.ConvLang).
		Int("slot", input.Slot).
		Bool("force_replace", input.ForceReplace)
	if res.DisplacedConversationID > 0 {
		logEvt = logEvt.Int64("displaced_conversation_id", res.DisplacedConversationID)
	}
	logEvt.Msg("Communauté publique épinglée par un administrateur")

	return community_recommendation_models.PinCommunityOutput{
		ConversationID:          input.ConversationID,
		Lang:                    res.ConvLang,
		PinSlot:                 input.Slot,
		DisplacedConversationID: res.DisplacedConversationID,
	}, nil
}
