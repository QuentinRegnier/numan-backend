package comment_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type UpdateCommentInput struct {
	CommentID int64  `json:"comment_id" binding:"required"`
	Content   string `json:"content" binding:"required,max=2200"`
}

// Validate vérifie les contraintes métier de mise à jour d'un commentaire.
func (p *UpdateCommentInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de mise à jour du commentaire est nul.", nil)
	}
	if p.CommentID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du commentaire doit être strictement positif.", nil)
	}
	trimmed := strings.TrimSpace(p.Content)
	if trimmed == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le commentaire ne peut pas être vide.", nil)
	}
	if utf8.RuneCountInString(trimmed) > 2200 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le commentaire ne peut pas dépasser 2200 caractères.", nil)
	}
	return nil
}
