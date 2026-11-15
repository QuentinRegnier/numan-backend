package conversation_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

type PinConversationInput struct {
	ConversationID int64 `json:"conversation_id" binding:"required"`
}

// Validate vérifie l'identifiant de conversation à épingler.
func (p *PinConversationInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload d'épinglage est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	return nil
}

type PinConversationOutput struct {
	InboxUpdateAt int64 `json:"inbox_update_at"`
}
