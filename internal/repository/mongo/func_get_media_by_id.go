package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"go.mongodb.org/mongo-driver/bson"
)

// MongoGetMediaByID récupère un média unitaire depuis MongoDB (L2) quel que soit son état de visibilité (Pending, Public, Quarantaine).
func MongoGetMediaByID(ctx context.Context, mediaID int64) (media_models.MediaPayload, error) {
	if Media == nil {
		return media_models.MediaPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Media' is not initialized"),
		)
	}
	docs, err := Media.Get(map[string]any{"id": mediaID}, nil)
	if err != nil {
		return media_models.MediaPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer le média (Storage L2).",
			err,
		)
	}
	if len(docs) == 0 {
		return media_models.MediaPayload{}, numan_error.NewNotFound(
			numan_error.CodeNotFound,
			"Média introuvable (Storage L2).",
			errors.New("media not found in Warm Storage (L2)"),
		)
	}
	var m media_models.MediaPayload
	if err := cursorDocToMedia(docs[0], &m); err != nil {
		return media_models.MediaPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de décoder le média (Storage L2).",
			err,
		)
	}
	return m, nil
}

func cursorDocToMedia(doc map[string]any, out *media_models.MediaPayload) error {
	raw, err := bson.Marshal(doc)
	if err != nil {
		return err
	}
	return bson.Unmarshal(raw, out)
}
