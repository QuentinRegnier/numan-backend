package moderation_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type ReviewBlameInput struct {
	ReportID  int64  `json:"report_id" binding:"required,gt=0"`
	Decision  string `json:"decision" binding:"required,oneof=validate oppose"`
	Rationale string `json:"rationale" binding:"required,min=3,max=1000"`
}

// Validate vérifie la décision et la justification d'une revue par un pair modérateur.
func (p *ReviewBlameInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête de revue de blâme est vide.", nil)
	}
	if p.ReportID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du signalement doit être strictement positif.", nil)
	}
	if p.Decision != "validate" && p.Decision != "oppose" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La décision de revue doit être 'validate' ou 'oppose'.", nil)
	}
	rLen := utf8.RuneCountInString(strings.TrimSpace(p.Rationale))
	if rLen < 3 || rLen > 1000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La justification de revue doit contenir entre 3 et 1000 caractères.", nil)
	}
	return nil
}
