package map_models

import (
	"fmt"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// DeletePOIInput contient la liste des identifiants de POIs à supprimer (`[]POI_IDs`).
type DeletePOIInput struct {
	POIIDs []int64 `json:"poi_ids" binding:"required,min=1"`
}

// Validate vérifie la conformité de la liste d'identifiants de POIs à supprimer.
func (p *DeletePOIInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de suppression des POIs est nul.", nil)
	}
	if len(p.POIIDs) == 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"La liste des identifiants de points d'intérêt à supprimer ne peut pas être vide.",
			nil,
		)
	}
	if len(p.POIIDs) > variables.MaxPOIDeleteBatch {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("La suppression simultanée est limitée à %d points d'intérêt.", variables.MaxPOIDeleteBatch),
			nil,
		)
	}
	for _, id := range p.POIIDs {
		if id <= 0 {
			return numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				"Tous les identifiants de points d'intérêt doivent être strictement positifs.",
				nil,
			)
		}
	}
	return nil
}

// DeletePOIOutput confirme la suppression synchrone L1 et l'ordonnancement asynchrone L3 des POIs.
type DeletePOIOutput struct {
	DeletedCount int     `json:"deleted_count"`
	POIIDs       []int64 `json:"poi_ids"`
}
