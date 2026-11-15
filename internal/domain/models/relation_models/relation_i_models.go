package relation_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

type RelationActionInput struct {
	TargetID int64 `json:"target_id" binding:"required"`
	Action   bool  `json:"action"`
}

// Validate vérifie que l'utilisateur cible est valide.
func (p *RelationActionInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	if p.TargetID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant cible doit être strictement positif.", nil)
	}
	return nil
}
