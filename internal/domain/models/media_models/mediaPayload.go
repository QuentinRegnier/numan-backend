package media_models

// MediaPayload représente l'entité d'un fichier média (image, vidéo) dans numan.
type MediaPayload struct {
	ID          int64   `json:"id" bson:"id" msgpack:"id"`
	OwnerID     int64   `json:"owner_id" bson:"owner_id" msgpack:"owner_id"`
	StoragePath string  `json:"storage_path" bson:"storage_path" msgpack:"storage_path"`
	Visibility  int     `json:"visibility" bson:"visibility" msgpack:"visibility"`
	IAScore     float64 `json:"ia_score" bson:"ia_score" msgpack:"ia_score"` // Probabilité NSFWJS Porn [0.0, 1.0]
	CreatedAt   int64   `json:"created_at" bson:"created_at" msgpack:"created_at"`
	UpdatedAt   int64   `json:"updated_at" bson:"updated_at" msgpack:"updated_at"`
}
