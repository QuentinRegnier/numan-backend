package moderation_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type CloseTicketInput struct {
	ReportID  int64  `json:"report_id" binding:"required,gt=0"`
	IsDismiss bool   `json:"is_dismiss"`
	Rationale string `json:"rationale" binding:"required,min=3,max=1000"`
}

// Validate vérifie la validité métier d'une demande de clôture de ticket.
func (p *CloseTicketInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête de clôture de ticket est vide.", nil)
	}
	if p.ReportID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du signalement doit être strictement positif.", nil)
	}
	rLen := utf8.RuneCountInString(strings.TrimSpace(p.Rationale))
	if rLen < 3 || rLen > 1000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La justification de clôture doit contenir entre 3 et 1000 caractères.", nil)
	}
	return nil
}

type CloseTicketOutput struct {
	ReportID   int64  `json:"report_id"`
	State      int    `json:"state"`
	Outcome    string `json:"outcome"`
	ArchivedAt int64  `json:"archived_at"`
}
