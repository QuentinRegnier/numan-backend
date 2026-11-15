package map_models

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// UpdatePOIItemInput définit les modifications partielles ou complètes à appliquer sur un POI existant.
type UpdatePOIItemInput struct {
	ID             int64    `json:"id" bson:"id" msgpack:"id" binding:"required,gt=0"`
	Type           *int16   `json:"type,omitempty" bson:"type,omitempty" msgpack:"type,omitempty"`
	Title          *string  `json:"title,omitempty" bson:"title,omitempty" msgpack:"title,omitempty"`
	Description    *string  `json:"description,omitempty" bson:"description,omitempty" msgpack:"description,omitempty"`
	Latitude       *float64 `json:"latitude,omitempty" bson:"latitude,omitempty" msgpack:"latitude,omitempty"`
	Longitude      *float64 `json:"longitude,omitempty" bson:"longitude,omitempty" msgpack:"longitude,omitempty"`
	Address        *string  `json:"address,omitempty" bson:"address,omitempty" msgpack:"address,omitempty"`
	Phone          *string  `json:"phone,omitempty" bson:"phone,omitempty" msgpack:"phone,omitempty"`
	Website        *string  `json:"website,omitempty" bson:"website,omitempty" msgpack:"website,omitempty"`
	Rating         *float64 `json:"rating,omitempty" bson:"rating,omitempty" msgpack:"rating,omitempty"`
	CommentCount   *int     `json:"comment_count,omitempty" bson:"comment_count,omitempty" msgpack:"comment_count,omitempty"`
	MediaIDs       *[]int64 `json:"media_ids,omitempty" bson:"media_ids,omitempty" msgpack:"media_ids,omitempty"`
	GoogleMapsLink *string  `json:"google_maps_link,omitempty" bson:"google_maps_link,omitempty" msgpack:"google_maps_link,omitempty"`
	AppleMapsLink  *string  `json:"apple_maps_link,omitempty" bson:"apple_maps_link,omitempty" msgpack:"apple_maps_link,omitempty"`
}

// Validate vérifie la validité des champs modifiés d'un POI.
func (p *UpdatePOIItemInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les données de mise à jour du POI sont nulles.", nil)
	}
	if p.ID <= 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Chaque point d'intérêt à modifier doit posséder un identifiant strictement positif.",
			nil,
		)
	}
	if p.Title != nil {
		title := strings.TrimSpace(*p.Title)
		if title == "" {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le titre du POI est obligatoire.", nil)
		}
		if utf8.RuneCountInString(title) > variables.MaxPOITitleLength {
			return numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				fmt.Sprintf("Le titre du POI ne peut pas dépasser %d caractères.", variables.MaxPOITitleLength),
				nil,
			)
		}
	}
	if p.Description != nil && utf8.RuneCountInString(strings.TrimSpace(*p.Description)) > variables.MaxPOIDescriptionLength {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("La description du POI ne peut pas dépasser %d caractères.", variables.MaxPOIDescriptionLength),
			nil,
		)
	}
	if p.Address != nil && utf8.RuneCountInString(strings.TrimSpace(*p.Address)) > variables.MaxPOIAddressLength {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("L'adresse du POI ne peut pas dépasser %d caractères.", variables.MaxPOIAddressLength),
			nil,
		)
	}
	if p.Phone != nil && utf8.RuneCountInString(strings.TrimSpace(*p.Phone)) > variables.MaxPOIPhoneLength {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("Le téléphone du POI ne peut pas dépasser %d caractères.", variables.MaxPOIPhoneLength),
			nil,
		)
	}
	if p.Website != nil && utf8.RuneCountInString(strings.TrimSpace(*p.Website)) > variables.MaxPOIWebsiteLength {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("L'URL du site web du POI ne peut pas dépasser %d caractères.", variables.MaxPOIWebsiteLength),
			nil,
		)
	}
	if p.Latitude != nil {
		lat := *p.Latitude
		if math.IsNaN(lat) || math.IsInf(lat, 0) || lat < variables.MinLatitude || lat > variables.MaxLatitude {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La latitude doit être comprise entre -90 et 90 degrés.", nil)
		}
	}
	if p.Longitude != nil {
		lng := *p.Longitude
		if math.IsNaN(lng) || math.IsInf(lng, 0) || lng < variables.MinLongitude || lng > variables.MaxLongitude {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La longitude doit être comprise entre -180 et 180 degrés.", nil)
		}
	}
	if p.Rating != nil {
		r := *p.Rating
		if math.IsNaN(r) || math.IsInf(r, 0) || r < variables.MinPOIRating || r > variables.MaxPOIRating {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La note (rating) doit être comprise entre 0.0 et 5.0.", nil)
		}
	}
	if p.CommentCount != nil && *p.CommentCount < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nombre de commentaires ne peut pas être négatif.", nil)
	}
	if p.MediaIDs != nil {
		if len(*p.MediaIDs) > variables.MaxMediaPerPOI {
			return numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				fmt.Sprintf("Un POI ne peut pas contenir plus de %d médias.", variables.MaxMediaPerPOI),
				nil,
			)
		}
		for _, mediaID := range *p.MediaIDs {
			if mediaID <= 0 {
				return numan_error.NewBadRequest(
					numan_error.CodeInvalidPayload,
					"Tous les identifiants de médias doivent être strictement positifs.",
					nil,
				)
			}
		}
	}
	return nil
}

// UpdatePOIInput permet la mise à jour d'un ou plusieurs POIs par un administrateur.
// Supporte un tableau `pois` ou les champs d'un POI unique à la racine du JSON.
type UpdatePOIInput struct {
	POIs           []UpdatePOIItemInput `json:"pois,omitempty"`
	ID             int64                `json:"id,omitempty" bson:"id,omitempty" msgpack:"id,omitempty"`
	Type           *int16               `json:"type,omitempty" bson:"type,omitempty" msgpack:"type,omitempty"`
	Title          *string              `json:"title,omitempty" bson:"title,omitempty" msgpack:"title,omitempty"`
	Description    *string              `json:"description,omitempty" bson:"description,omitempty" msgpack:"description,omitempty"`
	Latitude       *float64             `json:"latitude,omitempty" bson:"latitude,omitempty" msgpack:"latitude,omitempty"`
	Longitude      *float64             `json:"longitude,omitempty" bson:"longitude,omitempty" msgpack:"longitude,omitempty"`
	Address        *string              `json:"address,omitempty" bson:"address,omitempty" msgpack:"address,omitempty"`
	Phone          *string              `json:"phone,omitempty" bson:"phone,omitempty" msgpack:"phone,omitempty"`
	Website        *string              `json:"website,omitempty" bson:"website,omitempty" msgpack:"website,omitempty"`
	Rating         *float64             `json:"rating,omitempty" bson:"rating,omitempty" msgpack:"rating,omitempty"`
	CommentCount   *int                 `json:"comment_count,omitempty" bson:"comment_count,omitempty" msgpack:"comment_count,omitempty"`
	MediaIDs       *[]int64             `json:"media_ids,omitempty" bson:"media_ids,omitempty" msgpack:"media_ids,omitempty"`
	GoogleMapsLink *string              `json:"google_maps_link,omitempty" bson:"google_maps_link,omitempty" msgpack:"google_maps_link,omitempty"`
	AppleMapsLink  *string              `json:"apple_maps_link,omitempty" bson:"apple_maps_link,omitempty" msgpack:"apple_maps_link,omitempty"`
}

// Validate vérifie l'intégrité du lot de mise à jour des POIs.
func (p *UpdatePOIInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de mise à jour des POIs est nul.", nil)
	}
	items := p.NormalizeItems()
	if len(items) == 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Aucun point d'intérêt valide fourni pour la mise à jour.",
			nil,
		)
	}
	if len(items) > variables.MaxPOIUpdateBatch {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("La mise à jour simultanée est limitée à %d points d'intérêt.", variables.MaxPOIUpdateBatch),
			nil,
		)
	}
	seen := make(map[int64]struct{}, len(items))
	for i := range items {
		item := items[i]
		if err := item.Validate(); err != nil {
			return err
		}
		if _, dup := seen[item.ID]; dup {
			return numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				"Identifiant de point d'intérêt en doublon dans la requête de mise à jour.",
				nil,
			)
		}
		seen[item.ID] = struct{}{}
	}
	return nil
}

// NormalizeItems extrait la liste unifiée des POIs à mettre à jour depuis UpdatePOIInput.
func (in UpdatePOIInput) NormalizeItems() []UpdatePOIItemInput {
	if len(in.POIs) > 0 {
		return in.POIs
	}
	if in.ID != 0 {
		return []UpdatePOIItemInput{
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

// UpdatePOIOutput confirme la mise à jour d'un ou plusieurs POIs.
type UpdatePOIOutput struct {
	UpdatedCount int       `json:"updated_count"`
	POIIDs       []int64   `json:"poi_ids"`
	POIs         []POIView `json:"pois"`
}
