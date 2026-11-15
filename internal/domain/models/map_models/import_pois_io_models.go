package map_models

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

// ImportPOIsInput permet l'importation en masse de POIs par un administrateur.
type ImportPOIsInput struct {
	POIs []CreatePOIItemInput `json:"pois" binding:"required,min=1"`
}

// ImportPOIsOutput confirme l'importation en masse des POIs.
type ImportPOIsOutput struct {
	ImportedCount int       `json:"imported_count"`
	POIIDs        []int64   `json:"poi_ids"`
	POIs          []POIView `json:"pois"`
}
