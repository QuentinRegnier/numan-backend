package moderation_models

import (
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/report_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// BlameRecord trace un blâme individuel infligé à un utilisateur pour l'audit des 3 blâmes (§C.11).
type BlameRecord struct {
	ReportID        int64  `msgpack:"report_id" json:"report_id"`
	TargetType      int    `msgpack:"target_type" json:"target_type"`
	TargetID        int64  `msgpack:"target_id" json:"target_id"`
	ProposerModID   int64  `msgpack:"proposer_mod_id" json:"proposer_mod_id"`
	ValidatorModID  int64  `msgpack:"validator_mod_id" json:"validator_mod_id"`
	FinalVoterModID int64  `msgpack:"final_voter_mod_id,omitempty" json:"final_voter_mod_id,omitempty"`
	Motivation      string `msgpack:"motivation" json:"motivation"`
	CreatedAt       int64  `msgpack:"created_at" json:"created_at"`
}

// Validate vérifie la cohérence d'un enregistrement de blâme.
func (p *BlameRecord) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'enregistrement de blâme est nul.", nil)
	}
	if p.ReportID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du signalement du blâme ne peut pas être négatif.", nil)
	}
	if !report_models.IsValidReportTargetType(p.TargetType) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Type de cible du blâme invalide.", nil)
	}
	if p.TargetID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la cible du blâme ne peut pas être négatif.", nil)
	}
	if p.ProposerModID < 0 || p.ValidatorModID < 0 || p.FinalVoterModID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les identifiants des modérateurs du blâme ne peuvent pas être négatifs.", nil)
	}
	if p.ProposerModID == 0 && p.ValidatorModID == 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le blâme doit être rattaché à au moins un modérateur instructeur.", nil)
	}
	if utf8.RuneCountInString(p.Motivation) > 1000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La motivation du blâme ne peut pas dépasser 1000 caractères.", nil)
	}
	if p.CreatedAt < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'horodatage du blâme ne peut pas être négatif.", nil)
	}
	return nil
}
