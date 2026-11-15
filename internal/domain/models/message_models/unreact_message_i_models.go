package message_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// UnreactMessageInput valide la demande de retrait de réaction.
type UnreactMessageInput struct {
	MessageID int64 `json:"message_id" binding:"required"`
}

// Validate vérifie l'identifiant du message pour retirer la réaction.
func (p *UnreactMessageInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de retrait de réaction est nul.", nil)
	}
	if p.MessageID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du message doit être strictement positif.", nil)
	}
	return nil
}
