package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
)

// MongoLoadArticlesByIDs récupère un lot d'articles par leurs IDs depuis MongoDB L2.
func MongoLoadArticlesByIDs(ctx context.Context, ids []int64) ([]article_models.ArticlePayload, error) {
	if Articles == nil || Articles.DB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Articles' is not initialized"),
		)
	}
	if len(ids) == 0 {
		return []article_models.ArticlePayload{}, nil
	}

	filter := map[string]any{
		"id": map[string]any{"$in": ids},
	}
	docs, err := Articles.Get(filter, nil)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les articles depuis le stockage L2.",
			err,
		)
	}

	articles := make([]article_models.ArticlePayload, 0, len(docs))
	for _, doc := range docs {
		var a article_models.ArticlePayload
		if err := pkg.ToStruct(doc, &a); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de traiter les articles depuis le stockage L2.",
				err,
			)
		}
		a.Normalize()
		articles = append(articles, a)
	}
	return articles, nil
}
