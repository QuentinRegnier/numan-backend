package message_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// DeleteMessageInput valide la demande de suppression de message
type DeleteMessageInput struct {
	MessageID int64 `json:"message_id" binding:"required"`
}

// Validate vérifie l'identifiant du message à supprimer.
func (p *DeleteMessageInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de suppression de message est nul.", nil)
	}
	if p.MessageID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du message doit être strictement positif.", nil)
	}
	return nil
}

type DeleteMessageOutput struct {
	InboxUpdateAt int64 `json:"inbox_update_at"`
}
