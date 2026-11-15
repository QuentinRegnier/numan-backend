package moderation_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type ReplyTicketInfoInput struct {
	ReportID int64  `json:"report_id" binding:"required,gt=0"`
	Reply    string `json:"reply" binding:"required,min=1,max=2000"`
}

// Validate vérifie l'identifiant du ticket et la réponse du plaignant.
func (p *ReplyTicketInfoInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La réponse au complément d'information est vide.", nil)
	}
	if p.ReportID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du signalement doit être strictement positif.", nil)
	}
	rLen := utf8.RuneCountInString(strings.TrimSpace(p.Reply))
	if rLen < 1 || rLen > 2000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La réponse doit contenir entre 1 et 2000 caractères.", nil)
	}
	return nil
}
