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

// UnpinCommunity retire de manière idempotente l'épinglage d'une communauté publique.
func UnpinCommunity(
	ctx context.Context,
	adminID int64,
	input community_recommendation_models.UnpinCommunityInput,
) (community_recommendation_models.UnpinCommunityOutput, error) {
	if err := security_service.LeftAdmin(ctx, adminID); err != nil {
		return community_recommendation_models.UnpinCommunityOutput{}, err
	}
	if err := input.Validate(); err != nil {
		return community_recommendation_models.UnpinCommunityOutput{}, err
	}

	res, err := postgres.FuncUnsetCommunityPin(ctx, input.ConversationID)
	if err != nil {
		return community_recommendation_models.UnpinCommunityOutput{}, err
	}

	switch res.StatusCode {
	case "OK":
		// Continue
	case "NOT_FOUND":
		return community_recommendation_models.UnpinCommunityOutput{}, numan_error.NewNotFound(
			numan_error.CodeNotFound,
			"Communauté introuvable.",
			nil,
		)
	case "NOT_PUBLIC_COMMUNITY", "NOT_ACTIVE_PUBLIC_COMMUNITY":
		return community_recommendation_models.UnpinCommunityOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Seule une communauté publique peut être désépinglée.",
			nil,
		)
	default:
		return community_recommendation_models.UnpinCommunityOutput{}, numan_error.NewInternal()
	}

	unpinned := res.OldPinSlot > 0
	if unpinned {
		if errDel := cache_service.DeleteCommunityPinInCache(ctx, res.ConvLang, res.OldPinSlot); errDel != nil {
			numan_log.Warn(ctx).Err(errDel).Int64("conversation_id", input.ConversationID).Msg("Échec suppression pin en cache L1")
		}
		syncCommunityCachesAfterAdminMutation(ctx, input.ConversationID, func(c conversation_models.ConversationPayload) conversation_models.ConversationPayload {
			c.PinSlot = 0
			return c
		}, func(m lite_models.ConvLiteRequest) lite_models.ConvLiteRequest {
			m.PinSlot = 0
			return m
		})
	} else if res.ConvLang != "" {
		// Nettoyage défensif en cas de divergence L1/L3
		_ = cache_service.RemoveCommunityPinByConversationID(ctx, res.ConvLang, input.ConversationID)
	}

	numan_log.Info(ctx).
		Int64("admin_id", adminID).
		Int64("conversation_id", input.ConversationID).
		Str("lang", res.ConvLang).
		Bool("unpinned", unpinned).
		Msg("Désépinglage de communauté publique exécuté par un administrateur")

	return community_recommendation_models.UnpinCommunityOutput{
		ConversationID: input.ConversationID,
		Unpinned:       unpinned,
	}, nil
}
