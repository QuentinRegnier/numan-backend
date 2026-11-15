package moderation_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type FinalVoteBlameInput struct {
	ReportID  int64  `json:"report_id" binding:"required,gt=0"`
	Decision  string `json:"decision" binding:"required,oneof=confirm_blame reject_blame"`
	Rationale string `json:"rationale" binding:"required,min=3,max=1000"`
}

// Validate vérifie la décision d'arbitrage final et sa justification.
func (p *FinalVoteBlameInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête de vote d'arbitrage final est vide.", nil)
	}
	if p.ReportID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du signalement doit être strictement positif.", nil)
	}
	if p.Decision != "confirm_blame" && p.Decision != "reject_blame" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La décision d'arbitrage doit être 'confirm_blame' ou 'reject_blame'.", nil)
	}
	rLen := utf8.RuneCountInString(strings.TrimSpace(p.Rationale))
	if rLen < 3 || rLen > 1000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La justification d'arbitrage doit contenir entre 3 et 1000 caractères.", nil)
	}
	return nil
}
