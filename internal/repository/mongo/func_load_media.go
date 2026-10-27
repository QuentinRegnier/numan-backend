package mongo

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"go.mongodb.org/mongo-driver/bson"
)

func MongoLoadMedia(mediaIDs []int64) ([]media_models.MediaPayload, error) {
	if Media == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Media' is not initialized"),
		)
	}

	if len(mediaIDs) == 0 {
		return []media_models.MediaPayload{}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// On filtre sur visibility = 1 (MediaVisibilityPublic) pour ignorer les médias supprimés ou en quarantaine
	filter := bson.M{
		"id":         bson.M{"$in": mediaIDs},
		"visibility": variables.MediaVisibilityPublic,
	}

	cursor, err := Media.DB.Collection(Media.Name).Find(ctx, filter)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les médias.",
			err,
		)
	}
	defer cursor.Close(ctx)

	results := []media_models.MediaPayload{}
	if err = cursor.All(ctx, &results); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'extraire les médias de la requête (Storage L2).",
			err,
		)
	}

	return results, nil
}
