package community_recommendation_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// PinCommunityInput représente le corps JSON de POST /admin/communities/pin.
type PinCommunityInput struct {
	ConversationID int64 `json:"conversation_id" binding:"required"`
	Slot           int   `json:"slot" binding:"required"`
	ForceReplace   bool  `json:"force_replace"`
}

// Validate vérifie les bornes du slot d'épinglage (1..100) et l'identifiant de conversation.
func (in *PinCommunityInput) Validate() error {
	if in == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload d'épinglage est nul.", nil)
	}
	if in.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	if in.Slot < variables.CommunityRecoMinPinSlot || in.Slot > variables.CommunityRecoMaxPinSlot {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le slot d'épinglage doit être compris entre 1 et 100.", nil)
	}
	return nil
}

// PinCommunityOutput représente la réponse JSON de POST /admin/communities/pin.
// DisplacedConversationID vaut 0 (omis en JSON) si aucune autre communauté n'a été remplacée.
type PinCommunityOutput struct {
	ConversationID          int64  `json:"conversation_id"`
	Lang                    string `json:"lang"`
	PinSlot                 int    `json:"pin_slot"`
	DisplacedConversationID int64  `json:"displaced_conversation_id,omitempty"`
}
