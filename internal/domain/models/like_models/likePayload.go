package like_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

type LikePayload struct {
	ID         int64  `json:"id"`
	TargetType int    `json:"target_type"`
	TargetID   int64  `json:"target_id"` // Remplace post_id pour la généricité
	UserID     int64  `json:"user_id"`
	CreatedAt  string `json:"created_at"`
}

// Validate vérifie l'intégrité d'un LikePayload avant persistance.
func (p *LikePayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de like est nul.", nil)
	}
	if p.ID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du like doit être strictement positif.", nil)
	}
	if p.TargetType != variables.LikeTargetTypePost && p.TargetType != variables.LikeTargetTypeComment {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le type de cible du like est invalide.", nil)
	}
	if p.TargetID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la cible du like doit être strictement positif.", nil)
	}
	if p.UserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de l'utilisateur doit être strictement positif.", nil)
	}
	return nil
}
