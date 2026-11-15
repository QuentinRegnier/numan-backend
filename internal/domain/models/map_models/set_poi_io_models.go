package map_models

import (
	"fmt"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// SetPOIInput permet l'ajout d'un ou plusieurs POIs par un administrateur.
// Supporte un tableau `pois` ou les champs d'un POI unique à la racine du JSON pour flexibilité maximale.
type SetPOIInput struct {
	POIs           []CreatePOIItemInput `json:"pois,omitempty"`
	ID             int64                `json:"id,omitempty" bson:"id,omitempty" msgpack:"id,omitempty"`
	Type           int16                `json:"type,omitempty" bson:"type,omitempty" msgpack:"type,omitempty"`
	Title          string               `json:"title,omitempty" bson:"title,omitempty" msgpack:"title,omitempty"`
	Description    string               `json:"description,omitempty" bson:"description,omitempty" msgpack:"description,omitempty"`
	Latitude       float64              `json:"latitude,omitempty" bson:"latitude,omitempty" msgpack:"latitude,omitempty"`
	Longitude      float64              `json:"longitude,omitempty" bson:"longitude,omitempty" msgpack:"longitude,omitempty"`
	Address        string               `json:"address,omitempty" bson:"address,omitempty" msgpack:"address,omitempty"`
	Phone          string               `json:"phone,omitempty" bson:"phone,omitempty" msgpack:"phone,omitempty"`
	Website        string               `json:"website,omitempty" bson:"website,omitempty" msgpack:"website,omitempty"`
	Rating         float64              `json:"rating,omitempty" bson:"rating,omitempty" msgpack:"rating,omitempty"`
	CommentCount   int                  `json:"comment_count,omitempty" bson:"comment_count,omitempty" msgpack:"comment_count,omitempty"`
	MediaIDs       []int64              `json:"media_ids,omitempty" bson:"media_ids,omitempty" msgpack:"media_ids,omitempty"`
	GoogleMapsLink string               `json:"google_maps_link,omitempty" bson:"google_maps_link,omitempty" msgpack:"google_maps_link,omitempty"`
	AppleMapsLink  string               `json:"apple_maps_link,omitempty" bson:"apple_maps_link,omitempty" msgpack:"apple_maps_link,omitempty"`
}

// Validate vérifie les contraintes métier de la requête de création d'un ou plusieurs POIs.
func (p *SetPOIInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de création des POIs est nul.", nil)
	}
	items := p.NormalizeItems()
	if len(items) == 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Aucun point d'intérêt valide fourni pour la création.",
			nil,
		)
	}
	if len(items) > variables.MaxPOISetBatch {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("La création simultanée est limitée à %d points d'intérêt.", variables.MaxPOISetBatch),
			nil,
		)
	}
	seenIDs := make(map[int64]struct{}, len(items))
	for i := range items {
		item := items[i]
		if err := item.Validate(); err != nil {
			return err
		}
		if item.ID > 0 {
			if _, dup := seenIDs[item.ID]; dup {
				return numan_error.NewBadRequest(
					numan_error.CodeInvalidPayload,
					fmt.Sprintf("L'identifiant de point d'intérêt %d est présent en doublon dans le lot.", item.ID),
					nil,
				)
			}
			seenIDs[item.ID] = struct{}{}
		}
	}
	return nil
}

// NormalizeItems extrait la liste unifiée des POIs à créer depuis SetPOIInput.
func (in SetPOIInput) NormalizeItems() []CreatePOIItemInput {
	if len(in.POIs) > 0 {
		return in.POIs
	}
	if in.Title != "" || in.ID != 0 || in.Latitude != 0 || in.Longitude != 0 || in.Description != "" || in.Address != "" {
		return []CreatePOIItemInput{
			{
				ID:             in.ID,
				Type:           in.Type,
				Title:          in.Title,
				Description:    in.Description,
				Latitude:       in.Latitude,
				Longitude:      in.Longitude,
				Address:        in.Address,
				Phone:          in.Phone,
				Website:        in.Website,
				Rating:         in.Rating,
				CommentCount:   in.CommentCount,
				MediaIDs:       in.MediaIDs,
				GoogleMapsLink: in.GoogleMapsLink,
				AppleMapsLink:  in.AppleMapsLink,
			},
		}
	}
	return nil
}

// SetPOIOutput confirme la création d'un ou plusieurs POIs.
type SetPOIOutput struct {
	CreatedCount int       `json:"created_count"`
	POIIDs       []int64   `json:"poi_ids"`
	POIs         []POIView `json:"pois"`
}
