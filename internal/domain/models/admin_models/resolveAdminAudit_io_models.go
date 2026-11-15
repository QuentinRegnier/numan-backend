package admin_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type ResolveAdminAuditInput struct {
	ReportID              int64   `json:"report_id" binding:"required,gt=0"`
	Decision              string  `json:"decision" binding:"required,oneof=validate revoke"`
	Rationale             string  `json:"rationale" binding:"required,min=5,max=1000"`
	RevokedBlameReportIDs []int64 `json:"revoked_blame_report_ids,omitempty" binding:"omitempty,max=100,dive,gt=0"`
}

// Validate vérifie les invariants métier d'une résolution d'audit des 3 blâmes.
func (p *ResolveAdminAuditInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête de résolution d'audit est vide.", nil)
	}
	if p.ReportID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du rapport d'audit doit être strictement positif.", nil)
	}
	if p.Decision != "validate" && p.Decision != "revoke" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La décision d'audit doit être 'validate' ou 'revoke'.", nil)
	}
	rationaleLen := utf8.RuneCountInString(strings.TrimSpace(p.Rationale))
	if rationaleLen < 5 || rationaleLen > 1000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La justification de l'audit doit contenir entre 5 et 1000 caractères.", nil)
	}
	if len(p.RevokedBlameReportIDs) > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nombre d'identifiants de blâmes révoqués ne peut pas dépasser 100.", nil)
	}
	for _, id := range p.RevokedBlameReportIDs {
		if id <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Chaque identifiant de blâme révoqué doit être strictement positif.", nil)
		}
	}
	return nil
}

type ResolveAdminAuditOutput struct {
	Ticket          moderation_models.ModerationTicketPublicView `json:"ticket"`
	TargetBanned    bool                                         `json:"target_banned"`
	PenalizedModIDs []int64                                      `json:"penalized_mod_ids,omitempty"`
	DemotedModIDs   []int64                                      `json:"demoted_mod_ids,omitempty"`
	RewardedModIDs  []int64                                      `json:"rewarded_mod_ids,omitempty"`
}
