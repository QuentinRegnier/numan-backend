package message_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// UpdateMessageInput valide le nouveau contenu du message.
type UpdateMessageInput struct {
	MessageID int64  `json:"message_id" binding:"required"`
	Content   string `json:"content" binding:"required,max=2200"`
}

// Validate vérifie les règles métier d'édition d'un message.
func (p *UpdateMessageInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de mise à jour de message est nul.", nil)
	}
	if p.MessageID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du message doit être strictement positif.", nil)
	}
	content := strings.TrimSpace(p.Content)
	if content == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le message ne peut pas être vide.", nil)
	}
	if utf8.RuneCountInString(content) > 2200 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le message ne peut pas dépasser 2200 caractères.", nil)
	}
	return nil
}

type UpdateMessageOutput struct {
	InboxUpdateAt int64 `json:"inbox_update_at"`
}
