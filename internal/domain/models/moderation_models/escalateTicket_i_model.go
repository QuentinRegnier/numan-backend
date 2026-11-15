package moderation_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type EscalateTicketInput struct {
	ReportID        int64  `json:"report_id" binding:"required,gt=0"`
	EscalationType  string `json:"escalation_type" binding:"required,oneof=hard_ban_request cgu_gray_area"`
	Reason          string `json:"reason" binding:"required,min=5,max=1000"`
	CGUFeedbackNote string `json:"cgu_feedback_note,omitempty" binding:"omitempty,max=2000"`
}

// Validate vérifie les invariants d'une demande d'escalade vers l'administration.
func (p *EscalateTicketInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête d'escalade est vide.", nil)
	}
	if p.ReportID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du signalement doit être strictement positif.", nil)
	}
	if p.EscalationType != "hard_ban_request" && p.EscalationType != "cgu_gray_area" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le type d'escalade doit être 'hard_ban_request' ou 'cgu_gray_area'.", nil)
	}
	reasonLen := utf8.RuneCountInString(strings.TrimSpace(p.Reason))
	if reasonLen < 5 || reasonLen > 1000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le motif d'escalade doit contenir entre 5 et 1000 caractères.", nil)
	}
	if utf8.RuneCountInString(strings.TrimSpace(p.CGUFeedbackNote)) > 2000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La note explicative CGU ne peut pas dépasser 2000 caractères.", nil)
	}
	return nil
}
