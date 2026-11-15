package map_models

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
