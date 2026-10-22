package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/saved_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"go.mongodb.org/mongo-driver/bson"
)

func MongoLoadSavedPosts(ctx context.Context, userID int64, limit int64, offset int64) ([]saved_models.SavedPayload, error) {
	if Saved == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Saved' is not initialized"),
		)
	}

	filter := bson.M{"user_id": userID}
	sort := map[string]any{"created_at": -1}

	docs, err := Saved.GetPaginated(filter, sort, offset, limit)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les posts sauvegardés.",
			err,
		)
	}

	saveds := make([]saved_models.SavedPayload, 0, len(docs))
	for _, doc := range docs {
		var s saved_models.SavedPayload
		if err := pkg.ToStruct(doc, &s); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de traiter les posts sauvegardés.",
				err,
			)
		}
		saveds = append(saveds, s)
	}
	return saveds, nil
}
