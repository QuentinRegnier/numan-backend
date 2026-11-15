package admin_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type AdminHardBanInput struct {
	TargetUserID    int64  `json:"target_user_id" binding:"required,gt=0"`
	ReportID        int64  `json:"report_id,omitempty" binding:"omitempty,gte=0"`
	Reason          string `json:"reason" binding:"required,min=5,max=1000"`
	CGUFeedbackNote string `json:"cgu_feedback_note,omitempty" binding:"omitempty,max=2000"`
}

// Validate vérifie les invariants métier d'une demande de bannissement définitif.
func (p *AdminHardBanInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête de bannissement est vide.", nil)
	}
	if p.TargetUserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de l'utilisateur cible doit être strictement positif.", nil)
	}
	if p.ReportID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du signalement associé ne peut pas être négatif.", nil)
	}
	reasonLen := utf8.RuneCountInString(strings.TrimSpace(p.Reason))
	if reasonLen < 5 || reasonLen > 1000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le motif du bannissement doit contenir entre 5 et 1000 caractères.", nil)
	}
	if utf8.RuneCountInString(strings.TrimSpace(p.CGUFeedbackNote)) > 2000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La note explicative CGU ne peut pas dépasser 2000 caractères.", nil)
	}
	return nil
}

type AdminHardBanOutput struct {
	TargetUserID    int64   `json:"target_user_id"`
	Banned          bool    `json:"banned"`
	TrustScore      float64 `json:"trust_score"`
	CGUFeedbackNote string  `json:"cgu_feedback_note,omitempty"`
	ExecutedAt      int64   `json:"executed_at"`
}
