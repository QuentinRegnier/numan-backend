package message_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// MessagePayload correspond exactement au schéma Postgres messaging.messages
type MessagePayload struct {
	Attachments       map[string]any `json:"attachments" bson:"attachments"` // JSONB polymorphe
	Content           string         `json:"content" bson:"content"`
	ID                int64          `json:"id" bson:"id"`
	ConversationID    int64          `json:"conversation_id" bson:"conversation_id"`
	SenderID          int64          `json:"sender_id" bson:"sender_id"`
	MessageType       int            `json:"message_type" bson:"message_type"`
	CreatedAt         int64          `json:"created_at" bson:"created_at"`
	UpdatedAt         int64          `json:"updated_at" bson:"updated_at"`
	Visibility        bool           `json:"visibility" bson:"visibility"`
	AllowNewReactions bool           `json:"allow_new_reactions" bson:"allow_new_reactions"`
}

// Validate vérifie l'intégrité d'un MessagePayload avant mise en cache ou persistance.
func (p *MessagePayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload du message est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation du message doit être strictement positif.", nil)
	}
	if p.SenderID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de l'expéditeur doit être strictement positif.", nil)
	}
	if p.MessageType < variables.MessageTypeText || p.MessageType > variables.MessageTypeWidget {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Type de message inconnu.", nil)
	}
	if utf8.RuneCountInString(p.Content) > 2200 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le contenu du message ne peut pas dépasser 2200 caractères.", nil)
	}
	if p.MessageType == variables.MessageTypeText && p.Visibility && strings.TrimSpace(p.Content) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Un message texte actif ne peut pas être vide.", nil)
	}
	if p.MessageType == variables.MessageTypeWidget && p.Visibility {
		if p.Attachments == nil || p.Attachments["widget"] == nil {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le widget est manquant dans les pièces jointes.", nil)
		}
		wp, err := ParseWidgetPayload(p.Attachments["widget"])
		if err != nil {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload du widget est invalide.", err)
		}
		if err := wp.Validate(); err != nil {
			return err
		}
	}
	return nil
}
