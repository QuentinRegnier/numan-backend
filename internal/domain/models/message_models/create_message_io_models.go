package message_models

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// CreateMessageInput valide les données entrantes.
type CreateMessageInput struct {
	ConversationID int64          `json:"conversation_id" binding:"required"`
	MessageType    int            `json:"message_type" binding:"min=0,max=10"`
	Content        string         `json:"content" binding:"omitempty,max=2200"`
	Attachments    map[string]any `json:"attachments" binding:"omitempty"`
	ThreadParentID int64          `json:"thread_parent_id" binding:"omitempty"` // NOUVEAU
}

// Validate vérifie les règles métier d'envoi d'un message.
func (p *CreateMessageInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de création de message est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	if p.MessageType < variables.MessageTypeText || p.MessageType > variables.MessageTypeWidget {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Type de message inconnu.", nil)
	}
	if utf8.RuneCountInString(p.Content) > 2200 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le contenu du message ne peut pas dépasser 2200 caractères.", nil)
	}
	if p.ThreadParentID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du message parent ne peut pas être négatif.", nil)
	}
	switch p.MessageType {
	case variables.MessageTypeText:
		if strings.TrimSpace(p.Content) == "" {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Un message texte ne peut pas être vide.", nil)
		}
	case variables.MessageTypeMedia:
		if p.Attachments == nil {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du média est manquant dans les pièces jointes.", nil)
		}
		rawMediaID, hasMediaID := p.Attachments["media_id"]
		if !hasMediaID {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du média est manquant dans les pièces jointes.", nil)
		}
		var mediaID int64
		switch v := rawMediaID.(type) {
		case float64:
			mediaID = int64(v)
		case int64:
			mediaID = v
		case int:
			mediaID = int64(v)
		}
		if mediaID <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du média est invalide.", nil)
		}
		if rawIA, hasIA := p.Attachments["ia_score"]; hasIA && rawIA != nil {
			if iaScore, ok := rawIA.(float64); ok {
				if math.IsNaN(iaScore) || math.IsInf(iaScore, 0) || iaScore < 0.0 || iaScore > 1.0 {
					return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le score IA doit être compris entre 0.0 et 1.0.", nil)
				}
			}
		}
	case variables.MessageTypeWidget:
		if p.Attachments == nil || p.Attachments["widget"] == nil {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le widget est manquant dans les pièces jointes.", nil)
		}
		widgetPayload, errParse := ParseWidgetPayload(p.Attachments["widget"])
		if errParse != nil {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload du widget est invalide.", errParse)
		}
		if err := widgetPayload.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type CreateMessageOutput struct {
	MessageID     int64 `json:"message_id"`
	InboxUpdateAt int64 `json:"inbox_update_at"`
}
