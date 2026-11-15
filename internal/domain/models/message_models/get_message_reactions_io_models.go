package message_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// GetMessageReactionsInput valide les paramètres de la requête pour le Slow Path.
type GetMessageReactionsInput struct {
	MessageID       int64  `json:"message_id" form:"message_id" binding:"required"`
	Limit           int    `json:"limit" form:"limit" binding:"omitempty,min=1,max=100"`
	Offset          int    `json:"offset" form:"offset" binding:"omitempty,min=0"`
	BeforeCreatedAt *int64 `json:"before_created_at" form:"before_created_at"`
	BeforeID        *int64 `json:"before_id" form:"before_id"`
}

// Validate vérifie les paramètres de pagination des réactions d'un message.
func (p *GetMessageReactionsInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload des réactions est nul.", nil)
	}
	if p.MessageID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du message doit être strictement positif.", nil)
	}
	if p.Limit < 0 || p.Limit > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La limite doit être comprise entre 1 et 100.", nil)
	}
	if p.Offset < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'offset ne peut pas être négatif.", nil)
	}
	if p.BeforeCreatedAt != nil && *p.BeforeCreatedAt < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le curseur temporel ne peut pas être négatif.", nil)
	}
	if p.BeforeID != nil && *p.BeforeID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant curseur doit être strictement positif.", nil)
	}
	return nil
}

// UserReactionView combine les infos lite d'un utilisateur et sa réaction.
type UserReactionView struct {
	User     auth_models.UserLiteView `json:"user"`
	Reaction string                   `json:"reaction"`
}

// GetMessageReactionsOutput structure la réponse de l'API.
type GetMessageReactionsOutput struct {
	MessageID int64              `json:"message_id"`
	Reactions []UserReactionView `json:"reactions"`
}
