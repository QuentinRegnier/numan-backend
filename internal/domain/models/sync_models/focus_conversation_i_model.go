package sync_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

type FocusConversationInput struct {
	ConversationID int64 `json:"conversation_id" binding:"required"`
	Active         bool  `json:"active"` // true = est entré sur l'écran, false = en est sorti
}

// Validate vérifie l'identifiant de conversation pour le focus en temps réel.
func (p *FocusConversationInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de focus de conversation est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	return nil
}
