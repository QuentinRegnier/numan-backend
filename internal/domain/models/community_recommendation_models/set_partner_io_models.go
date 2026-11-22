package community_recommendation_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// SetPartnerInput représente le corps JSON de POST /admin/communities/partner.
type SetPartnerInput struct {
	ConversationID int64 `json:"conversation_id" binding:"required"`
	IsPartner      bool  `json:"is_partner"`
}

// Validate vérifie l'identifiant de conversation.
func (in *SetPartnerInput) Validate() error {
	if in == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload partenaire est nul.", nil)
	}
	if in.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	return nil
}

// SetPartnerOutput représente la réponse JSON de POST /admin/communities/partner.
type SetPartnerOutput struct {
	ConversationID int64 `json:"conversation_id"`
	IsPartner      bool  `json:"is_partner"`
}
