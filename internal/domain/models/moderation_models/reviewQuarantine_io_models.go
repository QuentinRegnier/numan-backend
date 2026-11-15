package moderation_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// ReviewQuarantineInput valide la décision d'un modérateur sur un média/post en quarantaine.
type ReviewQuarantineInput struct {
	MediaID  int64  `json:"media_id" binding:"required,gt=0"`
	PostID   int64  `json:"post_id" binding:"omitempty,gte=0"`
	Decision string `json:"decision" binding:"required,oneof=approve reject_minor reject_major"`
	Reason   string `json:"reason" binding:"max=500"`
}

// Validate vérifie les invariants métier d'une décision de revue de quarantaine.
func (p *ReviewQuarantineInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête de revue de quarantaine est vide.", nil)
	}
	if p.MediaID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du média doit être strictement positif.", nil)
	}
	if p.PostID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du post associé ne peut pas être négatif.", nil)
	}
	switch p.Decision {
	case "approve", "reject_minor", "reject_major":
	default:
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La décision doit être 'approve', 'reject_minor' ou 'reject_major'.", nil)
	}
	if utf8.RuneCountInString(strings.TrimSpace(p.Reason)) > 500 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le motif ne peut pas dépasser 500 caractères.", nil)
	}
	return nil
}

type ReviewQuarantineOutput struct {
	MediaID       int64  `json:"media_id"`
	NewVisibility int    `json:"new_visibility"`
	TrustEvent    string `json:"trust_event"`
	UpdatedAt     int64  `json:"updated_at"`
}
