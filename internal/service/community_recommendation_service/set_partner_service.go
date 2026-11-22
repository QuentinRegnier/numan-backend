package community_recommendation_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
)

// SetCommunityPartner active ou désactive le statut partenaire officiel d'une communauté publique.
func SetCommunityPartner(
	ctx context.Context,
	adminID int64,
	input community_recommendation_models.SetPartnerInput,
) (community_recommendation_models.SetPartnerOutput, error) {
	if err := security_service.LeftAdmin(ctx, adminID); err != nil {
		return community_recommendation_models.SetPartnerOutput{}, err
	}
	if err := input.Validate(); err != nil {
		return community_recommendation_models.SetPartnerOutput{}, err
	}

	res, err := postgres.FuncSetCommunityPartner(ctx, input.ConversationID, input.IsPartner)
	if err != nil {
		return community_recommendation_models.SetPartnerOutput{}, err
	}

	switch res.StatusCode {
	case "OK":
		// Continue
	case "NOT_FOUND":
		return community_recommendation_models.SetPartnerOutput{}, numan_error.NewNotFound(
			numan_error.CodeNotFound,
			"Communauté introuvable.",
			nil,
		)
	case "NOT_ACTIVE_PUBLIC_COMMUNITY":
		return community_recommendation_models.SetPartnerOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Seule une communauté publique active peut recevoir ou perdre le statut partenaire.",
			nil,
		)
	default:
		return community_recommendation_models.SetPartnerOutput{}, numan_error.NewInternal()
	}

	syncCommunityCachesAfterAdminMutation(ctx, input.ConversationID, func(c conversation_models.ConversationPayload) conversation_models.ConversationPayload {
		c.IsPartner = res.IsPartner
		return c
	}, func(m lite_models.ConvLiteRequest) lite_models.ConvLiteRequest {
		m.IsPartner = res.IsPartner
		return m
	})

	numan_log.Info(ctx).
		Int64("admin_id", adminID).
		Int64("conversation_id", input.ConversationID).
		Str("lang", res.ConvLang).
		Bool("is_partner", res.IsPartner).
		Msg("Statut partenaire de communauté publique modifié par un administrateur")

	return community_recommendation_models.SetPartnerOutput{
		ConversationID: input.ConversationID,
		IsPartner:      res.IsPartner,
	}, nil
}
