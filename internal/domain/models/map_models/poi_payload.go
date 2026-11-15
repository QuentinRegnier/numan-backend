package map_models

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// POIPayload représente l'entité interne d'un Point d'Intérêt (POI) de la carte de découverte.
// Persistée en PostgreSQL L3 (content.pois) et cachée en mémoire Redis L1 (POIs + POIsGeo).
type POIPayload struct {
	ID             int64   `json:"id" bson:"id" msgpack:"id"`
	Type           int16   `json:"type" bson:"type" msgpack:"type"`
	Title          string  `json:"title" bson:"title" msgpack:"title"`
	Description    string  `json:"description" bson:"description" msgpack:"description"`
	Latitude       float64 `json:"latitude" bson:"latitude" msgpack:"latitude"`
	Longitude      float64 `json:"longitude" bson:"longitude" msgpack:"longitude"`
	Address        string  `json:"address" bson:"address" msgpack:"address"`
	Phone          string  `json:"phone,omitempty" bson:"phone,omitempty" msgpack:"phone,omitempty"`
	Website        string  `json:"website,omitempty" bson:"website,omitempty" msgpack:"website,omitempty"`
	Rating         float64 `json:"rating" bson:"rating" msgpack:"rating"`
	CommentCount   int     `json:"comment_count" bson:"comment_count" msgpack:"comment_count"`
	MediaIDs       []int64 `json:"media_ids" bson:"media_ids" msgpack:"media_ids"`
	GoogleMapsLink string  `json:"google_maps_link,omitempty" bson:"google_maps_link,omitempty" msgpack:"google_maps_link,omitempty"`
	AppleMapsLink  string  `json:"apple_maps_link,omitempty" bson:"apple_maps_link,omitempty" msgpack:"apple_maps_link,omitempty"`
	Visibility     int     `json:"visibility" bson:"visibility" msgpack:"visibility"`
	CreatedAt      int64   `json:"created_at,omitempty" bson:"created_at" msgpack:"created_at"`
	UpdatedAt      int64   `json:"updated_at,omitempty" bson:"updated_at" msgpack:"updated_at"`
}

// Validate vérifie l'intégrité métier complète d'un POIPayload avant insertion ou mise à jour.
func (p *POIPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload POI est nul.", nil)
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
	if utf8.RuneCountInString(p.Description) > variables.MaxPOIDescriptionLength {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("La description du POI ne peut pas dépasser %d caractères.", variables.MaxPOIDescriptionLength),
			nil,
		)
	}
	if utf8.RuneCountInString(p.Address) > variables.MaxPOIAddressLength {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("L'adresse du POI ne peut pas dépasser %d caractères.", variables.MaxPOIAddressLength),
			nil,
		)
	}
	if utf8.RuneCountInString(p.Phone) > variables.MaxPOIPhoneLength {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("Le téléphone du POI ne peut pas dépasser %d caractères.", variables.MaxPOIPhoneLength),
			nil,
		)
	}
	if utf8.RuneCountInString(p.Website) > variables.MaxPOIWebsiteLength {
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
	if p.Visibility != variables.POIVisibilityPublic && p.Visibility != variables.POIVisibilitySoftDeleted {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Le statut de visibilité du POI est invalide.",
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
