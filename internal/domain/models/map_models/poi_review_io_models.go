package map_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
)

// CreatePOIReviewInput définit le contrat d'entrée pour créer ou mettre à jour une note/avis sur un POI.
type CreatePOIReviewInput struct {
	POIID   int64  `json:"poi_id" binding:"required,gt=0"`
	Rating  int8   `json:"rating" binding:"required,min=1,max=5"`
	Content string `json:"content,omitempty"`
}

// POIReviewView représente un avis enrichi (avec identité publique de l'auteur) retourné par l'API.
type POIReviewView struct {
	ID             int64                  `json:"id"`
	POIID          int64                  `json:"poi_id"`
	UserID         int64                  `json:"user_id"`
	Rating         int8                   `json:"rating"`
	Content        string                 `json:"content"`
	AuthorUsername string                 `json:"author_username,omitempty"`
	AuthorAvatar   media_models.MediaView `json:"author_avatar"`
	CreatedAt      int64                  `json:"created_at"`
	UpdatedAt      int64                  `json:"updated_at"`
}

// ToView convertit un POIReviewPayload et les informations d'auteur en POIReviewView.
func (r POIReviewPayload) ToView(authorUsername string, authorAvatar media_models.MediaView) POIReviewView {
	return POIReviewView{
		ID:             r.ID,
		POIID:          r.POIID,
		UserID:         r.UserID,
		Rating:         r.Rating,
		Content:        r.Content,
		AuthorUsername: authorUsername,
		AuthorAvatar:   authorAvatar,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}

// CreatePOIReviewOutput confirme l'enregistrement ou la mise à jour de l'avis/note et expose les compteurs mis à jour du POI.
type CreatePOIReviewOutput struct {
	Review          POIReviewView `json:"review"`
	POIRating       float64       `json:"poi_rating"`
	POICommentCount int           `json:"poi_comment_count"`
	IsUpdate        bool          `json:"is_update"`
}

// GetPOIReviewsInput définit la pagination des avis textuels d'un POI.
type GetPOIReviewsInput struct {
	POIID  int64 `json:"poi_id" form:"poi_id" binding:"required,gt=0"`
	Limit  int   `json:"limit" form:"limit"`
	Offset int   `json:"offset" form:"offset"`
}

// GetPOIReviewsOutput contient la liste paginée des avis textuels d'un POI (excluant les notes pures sans texte).
type GetPOIReviewsOutput struct {
	Reviews []POIReviewView `json:"reviews"`
}

// DeletePOIReviewInput identifie le POI dont l'utilisateur souhaite supprimer son avis/note.
type DeletePOIReviewInput struct {
	POIID int64 `json:"poi_id" binding:"required,gt=0"`
}

// DeletePOIReviewOutput confirme la suppression de l'avis/note et retourne les compteurs recalculés du POI.
type DeletePOIReviewOutput struct {
	POIID           int64   `json:"poi_id"`
	Deleted         bool    `json:"deleted"`
	POIRating       float64 `json:"poi_rating"`
	POICommentCount int     `json:"poi_comment_count"`
}
