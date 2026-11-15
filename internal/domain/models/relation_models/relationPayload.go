package relation_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

type RelationPayload struct {
	ID          int64 `json:"id" bson:"id"`
	PrimaryID   int64 `json:"primary_id" bson:"primary_id"`
	SecondaryID int64 `json:"secondary_id" bson:"secondary_id"`
	State       int   `json:"state" bson:"state"`
	CreatedAt   int64 `json:"created_at" bson:"created_at"`
	UpdatedAt   int64 `json:"updated_at" bson:"updated_at"`
}

// Validate vérifie les invariants métier d'une relation entre deux utilisateurs.
func (p *RelationPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de relation est nul.", nil)
	}
	if p.ID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de relation est invalide.", nil)
	}
	if p.PrimaryID <= 0 || p.SecondaryID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les identifiants utilisateurs de la relation doivent être strictement positifs.", nil)
	}
	if p.PrimaryID == p.SecondaryID {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Une relation avec soi-même est impossible.", nil)
	}
	switch p.State {
	case variables.RelationStateBlocked, variables.RelationStateNone, variables.RelationStateFollow, variables.RelationStateFriend:
		return nil
	default:
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'état de relation est invalide.", nil)
	}
}

// RelationUserView enrichit le profil avec l'état de relation du point de vue de celui qui fait la requête
type RelationUserView struct {
	auth_models.UserLiteView
	ViewerRelationState int `json:"viewer_relation_state"` // 0=Rien, 1=Abonné, 2=Ami, -1=Bloqué
}
