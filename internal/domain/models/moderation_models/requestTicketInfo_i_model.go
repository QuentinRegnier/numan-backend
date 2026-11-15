package moderation_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type RequestTicketInfoInput struct {
	ReportID int64  `json:"report_id" binding:"required,gt=0"`
	Question string `json:"question" binding:"required,min=3,max=1000"`
}

// Validate vérifie l'identifiant du ticket et la question posée au plaignant.
func (p *RequestTicketInfoInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La demande de complément d'information est vide.", nil)
	}
	if p.ReportID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du signalement doit être strictement positif.", nil)
	}
	qLen := utf8.RuneCountInString(strings.TrimSpace(p.Question))
	if qLen < 3 || qLen > 1000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La question doit contenir entre 3 et 1000 caractères.", nil)
	}
	return nil
}
