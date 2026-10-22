package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
)

// MongoLoadPosts récupère une liste de posts en fonction de leurs IDs (Niveau 2 Fallback)
func MongoLoadPosts(ctx context.Context, ids []int64) ([]post_models.PostPayload, error) {
	if Posts == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Posts' is not initialized"),
		)
	}

	if len(ids) == 0 {
		return []post_models.PostPayload{}, nil
	}

	filter := map[string]any{
		"id": map[string]any{"$in": ids},
	}

	docs, err := Posts.Get(filter, nil)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les posts.",
			err,
		)
	}

	posts := make([]post_models.PostPayload, 0, len(docs))
	for _, doc := range docs {
		var p post_models.PostPayload
		if err := pkg.ToStruct(doc, &p); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de traiter les posts.",
				err,
			)
		}
		posts = append(posts, p)
	}

	return posts, nil
}
