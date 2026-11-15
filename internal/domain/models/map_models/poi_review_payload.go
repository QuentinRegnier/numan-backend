package map_models

import (
	"fmt"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// POIReviewPayload représente un avis/note unique d'un utilisateur sur un Point d'Intérêt (content.poi_reviews).
// Une note sans texte est stockée de manière unifiée avec Content = "".
type POIReviewPayload struct {
	ID         int64  `json:"id" bson:"id" msgpack:"id"`
	POIID      int64  `json:"poi_id" bson:"poi_id" msgpack:"poi_id"`
	UserID     int64  `json:"user_id" bson:"user_id" msgpack:"user_id"`
	Rating     int8   `json:"rating" bson:"rating" msgpack:"rating"`
	Content    string `json:"content" bson:"content" msgpack:"content"`
	Visibility int    `json:"visibility" bson:"visibility" msgpack:"visibility"`
	CreatedAt  int64  `json:"created_at" bson:"created_at" msgpack:"created_at"`
	UpdatedAt  int64  `json:"updated_at" bson:"updated_at" msgpack:"updated_at"`
}

// Validate vérifie les contraintes métier d'un POIReviewPayload avant mise en cache ou persistance.
func (r *POIReviewPayload) Validate() error {
	if r == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload d'avis POI est nul.", nil)
	}
	if r.POIID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du point d'intérêt (poi_id) est obligatoire.", nil)
	}
	if r.UserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de l'utilisateur est obligatoire.", nil)
	}
	if r.Rating < int8(variables.MinPOIReviewRating) || r.Rating > int8(variables.MaxPOIReviewRating) {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("La note (rating) doit être un entier compris entre %d et %d.", variables.MinPOIReviewRating, variables.MaxPOIReviewRating),
			nil,
		)
	}
	if utf8.RuneCountInString(r.Content) > variables.MaxPOIReviewContentLength {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("Le commentaire ne peut pas dépasser %d caractères.", variables.MaxPOIReviewContentLength),
			nil,
		)
	}
	if r.Visibility != variables.POIReviewVisibilityPublic && r.Visibility != variables.POIReviewVisibilitySoftDeleted {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Le statut de visibilité de l'avis POI est invalide.",
			nil,
		)
	}
	return nil
}
