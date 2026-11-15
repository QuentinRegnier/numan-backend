package message_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// MessageReactionPayload correspond au schéma Postgres messaging.message_reactions
type MessageReactionPayload struct {
	ID        int64  `json:"id" bson:"id"`
	MessageID int64  `json:"message_id" bson:"message_id"`
	UserID    int64  `json:"user_id" bson:"user_id"`
	Reaction  string `json:"reaction" bson:"reaction"`
	CreatedAt int64  `json:"created_at" bson:"created_at"`
}

// Validate vérifie l'intégrité d'une réaction à un message.
func (p *MessageReactionPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de réaction est nul.", nil)
	}
	if p.MessageID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du message doit être strictement positif.", nil)
	}
	if p.UserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur doit être strictement positif.", nil)
	}
	reaction := strings.TrimSpace(p.Reaction)
	runeCount := utf8.RuneCountInString(reaction)
	if runeCount < 1 || runeCount > 30 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La réaction doit contenir entre 1 et 30 caractères.", nil)
	}
	return nil
}
