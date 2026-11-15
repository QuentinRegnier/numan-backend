package conversation_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// UpdateConversationInput valide les champs modifiables d'une conversation (PUT)
type UpdateConversationInput struct {
	ConversationID int64                `json:"conversation_id" binding:"required"`
	Title          string               `json:"title" binding:"max=100"`
	Settings       ConversationSettings `json:"settings"`
	Description    string               `json:"description"`
	AvatarID       int64                `json:"avatar_id"`
	ExternalLink   models.ExternalLinks `json:"external_link" binding:"omitempty"` // ✅ NOUVEAU
}

// Validate vérifie les contraintes métier de mise à jour d'une conversation.
func (p *UpdateConversationInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de mise à jour de conversation est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	if utf8.RuneCountInString(strings.TrimSpace(p.Title)) > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le titre de la conversation ne peut pas dépasser 100 caractères.", nil)
	}
	if utf8.RuneCountInString(p.Description) > 500 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La description de la conversation ne peut pas dépasser 500 caractères.", nil)
	}
	if p.AvatarID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant d'avatar ne peut pas être négatif.", nil)
	}
	if err := p.Settings.Validate(); err != nil {
		return err
	}
	if err := p.ExternalLink.Validate(); err != nil {
		return err
	}
	return nil
}

type UpdateConversationOutput struct {
	InboxUpdateAt int64 `json:"inbox_update_at"`
}
