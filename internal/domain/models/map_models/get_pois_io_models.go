package map_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// POIView représente la vue publique enrichie d'un Point d'Intérêt renvoyée au client mobile/web.
type POIView struct {
	ID             int64                    `json:"id"`
	Type           int16                    `json:"type"`
	Title          string                   `json:"title"`
	Description    string                   `json:"description"`
	Latitude       float64                  `json:"latitude"`
	Longitude      float64                  `json:"longitude"`
	Address        string                   `json:"address"`
	Phone          string                   `json:"phone,omitempty"`
	Website        string                   `json:"website,omitempty"`
	Rating         float64                  `json:"rating"`
	CommentCount   int                      `json:"comment_count"`
	MediaIDs       []int64                  `json:"media_ids"`
	Media          []media_models.MediaView `json:"media,omitempty"`
	GoogleMapsLink string                   `json:"google_maps_link,omitempty"`
	AppleMapsLink  string                   `json:"apple_maps_link,omitempty"`
}

// ToView convertit un POIPayload et ses médias résolus en POIView.
func (p POIPayload) ToView(media []media_models.MediaView) POIView {
	mediaIDs := p.MediaIDs
	if mediaIDs == nil {
		mediaIDs = []int64{}
	}
	if media == nil {
		media = []media_models.MediaView{}
	}
	return POIView{
		ID:             p.ID,
		Type:           p.Type,
		Title:          p.Title,
		Description:    p.Description,
		Latitude:       p.Latitude,
		Longitude:      p.Longitude,
		Address:        p.Address,
		Phone:          p.Phone,
		Website:        p.Website,
		Rating:         p.Rating,
		CommentCount:   p.CommentCount,
		MediaIDs:       mediaIDs,
		Media:          media,
		GoogleMapsLink: p.GoogleMapsLink,
		AppleMapsLink:  p.AppleMapsLink,
	}
}

// GetPoisInBBoxInput définit les coordonnées de la Bounding Box (Sud-Ouest et Nord-Est) pour la recherche spatiale.
type GetPoisInBBoxInput struct {
	SWLat float64 `json:"sw_lat"`
	SWLng float64 `json:"sw_lng"`
	NELat float64 `json:"ne_lat"`
	NELng float64 `json:"ne_lng"`
}

// Validate vérifie les coordonnées et la cohérence géométrique de la Bounding Box.
func (p *GetPoisInBBoxInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les coordonnées de la zone géographique sont nulles.", nil)
	}
	return ValidateBBox(p.SWLat, p.SWLng, p.NELat, p.NELng)
}

// GetPoisInBBoxOutput contient la liste des POIs trouvés dans la Bounding Box demandée.
type GetPoisInBBoxOutput struct {
	POIs []POIView `json:"pois"`
}
