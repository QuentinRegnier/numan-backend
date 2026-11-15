package moderation_models

import (
	"math"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ModeratorStatsRecord stocke l'état de gouvernance d'un modérateur ou d'un utilisateur ciblé en cache L1.
type ModeratorStatsRecord struct {
	UserID              int64         `msgpack:"user_id" json:"user_id"`
	ModeratorTrustScore float64       `msgpack:"moderator_trust_score" json:"moderator_trust_score"`
	Grade               int           `msgpack:"grade" json:"grade"`
	BlameCount          int           `msgpack:"blame_count" json:"blame_count"`
	BlameHistory        []BlameRecord `msgpack:"blame_history" json:"blame_history"`
	UpdatedAt           int64         `msgpack:"updated_at" json:"updated_at"`
}

// Validate vérifie les invariants d'un enregistrement de statistiques modérateur/cible.
func (p *ModeratorStatsRecord) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'enregistrement de statistiques modérateur est nul.", nil)
	}
	if p.UserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur doit être strictement positif.", nil)
	}
	if math.IsNaN(p.ModeratorTrustScore) || math.IsInf(p.ModeratorTrustScore, 0) || p.ModeratorTrustScore < 0.0 || p.ModeratorTrustScore > 1.0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le score de fiabilité modérateur doit être compris entre 0 et 1.", nil)
	}
	switch p.Grade {
	case variables.UserGradeUser,
		variables.UserGradeCertified,
		variables.UserGradeParter,
		variables.UserGradeModerator,
		variables.UserGradeAdmin:
	default:
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Grade utilisateur invalide.", nil)
	}
	if p.BlameCount < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le compteur de blâmes ne peut pas être négatif.", nil)
	}
	if p.UpdatedAt < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'horodatage de mise à jour ne peut pas être négatif.", nil)
	}
	for i := range p.BlameHistory {
		if err := p.BlameHistory[i].Validate(); err != nil {
			return err
		}
	}
	return nil
}
