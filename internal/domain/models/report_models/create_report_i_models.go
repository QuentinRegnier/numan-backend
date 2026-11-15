package report_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// CreateReportInput valide la requête de l'utilisateur.
type CreateReportInput struct {
	TargetType int     `json:"target_type" binding:"oneof=0 1 2 3 5"`
	TargetIDs  []int64 `json:"target_ids" binding:"required,min=1,max=20,dive,gt=0"` // Max 20 messages d'un coup
	Category   int     `json:"category" binding:"required,oneof=1 2 3 4 5 6 7 8 9 10 99"`
	Reason     string  `json:"reason" binding:"max=1000"` // Explications optionnelles de l'utilisateur
}

// Validate vérifie la validité métier d'une demande de création de signalement.
func (p *CreateReportInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête de signalement est vide.", nil)
	}
	if !IsValidReportTargetType(p.TargetType) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Type de cible de signalement invalide.", nil)
	}
	maxTargets := variables.MaxReportTargets
	if maxTargets <= 0 {
		maxTargets = 20
	}
	if len(p.TargetIDs) == 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Au moins un identifiant cible est requis.", nil)
	}
	if len(p.TargetIDs) > maxTargets {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nombre maximal de cibles par signalement est dépassé.", nil)
	}
	for _, id := range p.TargetIDs {
		if id <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Chaque identifiant cible doit être strictement positif.", nil)
		}
	}
	if !IsValidReportCategory(p.Category) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Catégorie de signalement invalide.", nil)
	}
	if utf8.RuneCountInString(strings.TrimSpace(p.Reason)) > 1000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le motif du signalement ne peut pas dépasser 1000 caractères.", nil)
	}
	return nil
}
