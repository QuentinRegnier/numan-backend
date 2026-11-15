package media_models

import (
	"math"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// MediaPayload représente l'entité d'un fichier média (image, vidéo) dans numan.
type MediaPayload struct {
	ID          int64   `json:"id" bson:"id" msgpack:"id"`
	OwnerID     int64   `json:"owner_id" bson:"owner_id" msgpack:"owner_id"`
	StoragePath string  `json:"storage_path" bson:"storage_path" msgpack:"storage_path"`
	Visibility  int     `json:"visibility" bson:"visibility" msgpack:"visibility"`
	IAScore     float64 `json:"ia_score" bson:"ia_score" msgpack:"ia_score"` // Probabilité NSFWJS Porn [0.0, 1.0]
	CreatedAt   int64   `json:"created_at" bson:"created_at" msgpack:"created_at"`
	UpdatedAt   int64   `json:"updated_at" bson:"updated_at" msgpack:"updated_at"`
}

// Validate vérifie l'intégrité métier d'un MediaPayload avant mise en cache ou persistance.
func (p *MediaPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload média est nul.", nil)
	}
	if p.ID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du média doit être strictement positif.", nil)
	}
	if p.OwnerID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du propriétaire du média doit être strictement positif.", nil)
	}
	if strings.TrimSpace(p.StoragePath) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le chemin de stockage du média est obligatoire.", nil)
	}
	switch p.Visibility {
	case variables.MediaVisibilityRejected,
		variables.MediaVisibilitySoftDeleted,
		variables.MediaVisibilityPending,
		variables.MediaVisibilityPublic,
		variables.MediaVisibilityQuarantine:
	default:
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le statut de visibilité du média est invalide.", nil)
	}
	if math.IsNaN(p.IAScore) || math.IsInf(p.IAScore, 0) || p.IAScore < 0.0 || p.IAScore > 1.0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le score IA doit être compris entre 0.0 et 1.0.", nil)
	}
	return nil
}
