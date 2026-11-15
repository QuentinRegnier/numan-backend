package moderation_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type ProposeBlameInput struct {
	ReportID   int64  `json:"report_id" binding:"required,gt=0"`
	Motivation string `json:"motivation" binding:"required,min=5,max=1000"`
}

// Validate vérifie l'identifiant et la motivation d'une proposition de blâme Four-Eyes.
func (p *ProposeBlameInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La proposition de blâme est vide.", nil)
	}
	if p.ReportID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du signalement doit être strictement positif.", nil)
	}
	motLen := utf8.RuneCountInString(strings.TrimSpace(p.Motivation))
	if motLen < 5 || motLen > 1000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La motivation du blâme doit contenir entre 5 et 1000 caractères.", nil)
	}
	return nil
}
