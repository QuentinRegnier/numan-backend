package map_models

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// CreatePOIItemInput définit les attributs d'un POI à créer ou importer.
type CreatePOIItemInput struct {
	ID             int64   `json:"id,omitempty" bson:"id,omitempty" msgpack:"id,omitempty"`
	Type           int16   `json:"type" bson:"type" msgpack:"type"`
	Title          string  `json:"title" bson:"title" msgpack:"title" binding:"required"`
	Description    string  `json:"description" bson:"description" msgpack:"description"`
	Latitude       float64 `json:"latitude" bson:"latitude" msgpack:"latitude"`
	Longitude      float64 `json:"longitude" bson:"longitude" msgpack:"longitude"`
	Address        string  `json:"address" bson:"address" msgpack:"address"`
	Phone          string  `json:"phone,omitempty" bson:"phone,omitempty" msgpack:"phone,omitempty"`
	Website        string  `json:"website,omitempty" bson:"website,omitempty" msgpack:"website,omitempty"`
	Rating         float64 `json:"rating,omitempty" bson:"rating,omitempty" msgpack:"rating,omitempty"`
	CommentCount   int     `json:"comment_count,omitempty" bson:"comment_count,omitempty" msgpack:"comment_count,omitempty"`
	MediaIDs       []int64 `json:"media_ids,omitempty" bson:"media_ids,omitempty" msgpack:"media_ids,omitempty"`
	GoogleMapsLink string  `json:"google_maps_link,omitempty" bson:"google_maps_link,omitempty" msgpack:"google_maps_link,omitempty"`
	AppleMapsLink  string  `json:"apple_maps_link,omitempty" bson:"apple_maps_link,omitempty" msgpack:"apple_maps_link,omitempty"`
}

// Validate vérifie les contraintes métier d'un élément de création/importation de POI.
func (p *CreatePOIItemInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les données du point d'intérêt sont nulles.", nil)
	}
	if p.ID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant d'un point d'intérêt ne peut pas être négatif.", nil)
	}
	title := strings.TrimSpace(p.Title)
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
	if utf8.RuneCountInString(strings.TrimSpace(p.Description)) > variables.MaxPOIDescriptionLength {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("La description du POI ne peut pas dépasser %d caractères.", variables.MaxPOIDescriptionLength),
			nil,
		)
	}
	if utf8.RuneCountInString(strings.TrimSpace(p.Address)) > variables.MaxPOIAddressLength {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("L'adresse du POI ne peut pas dépasser %d caractères.", variables.MaxPOIAddressLength),
			nil,
		)
	}
	if utf8.RuneCountInString(strings.TrimSpace(p.Phone)) > variables.MaxPOIPhoneLength {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("Le téléphone du POI ne peut pas dépasser %d caractères.", variables.MaxPOIPhoneLength),
			nil,
		)
	}
	if utf8.RuneCountInString(strings.TrimSpace(p.Website)) > variables.MaxPOIWebsiteLength {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("L'URL du site web du POI ne peut pas dépasser %d caractères.", variables.MaxPOIWebsiteLength),
			nil,
		)
	}
	if err := ValidateCoordinates(p.Latitude, p.Longitude); err != nil {
		return err
	}
	if math.IsNaN(p.Rating) || math.IsInf(p.Rating, 0) || p.Rating < variables.MinPOIRating || p.Rating > variables.MaxPOIRating {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"La note (rating) doit être comprise entre 0.0 et 5.0.",
			nil,
		)
	}
	if p.CommentCount < 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Le nombre de commentaires ne peut pas être négatif.",
			nil,
		)
	}
	if len(p.MediaIDs) > variables.MaxMediaPerPOI {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("Un POI ne peut pas contenir plus de %d médias.", variables.MaxMediaPerPOI),
			nil,
		)
	}
	for _, mediaID := range p.MediaIDs {
		if mediaID <= 0 {
			return numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				"Tous les identifiants de médias doivent être strictement positifs.",
				nil,
			)
		}
	}
	return nil
}

// ImportPOIsInput permet l'importation en masse de POIs par un administrateur.
type ImportPOIsInput struct {
	POIs []CreatePOIItemInput `json:"pois" binding:"required,min=1"`
}

// Validate vérifie l'intégrité du lot d'importation de POIs.
func (p *ImportPOIsInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload d'importation des POIs est nul.", nil)
	}
	if len(p.POIs) == 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"La liste des points d'intérêt à importer ne peut pas être vide.",
			nil,
		)
	}
	if len(p.POIs) > variables.MaxPOIImportBatch {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("L'import est limité à %d points d'intérêt par requête.", variables.MaxPOIImportBatch),
			nil,
		)
	}
	seenIDs := make(map[int64]struct{}, len(p.POIs))
	for i := range p.POIs {
		item := p.POIs[i]
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

// ImportPOIsOutput confirme l'importation en masse des POIs.
type ImportPOIsOutput struct {
	ImportedCount int       `json:"imported_count"`
	POIIDs        []int64   `json:"poi_ids"`
	POIs          []POIView `json:"pois"`
}
