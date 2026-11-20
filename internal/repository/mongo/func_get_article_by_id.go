package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
)

// MongoGetArticleByID récupère un article unique par son ID depuis le Warm Storage MongoDB (L2).
func MongoGetArticleByID(ctx context.Context, articleID int64) (article_models.ArticlePayload, bool, error) {
	if Articles == nil || Articles.DB == nil {
		return article_models.ArticlePayload{}, false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Articles' is not initialized"),
		)
	}
	if articleID <= 0 {
		return article_models.ArticlePayload{}, false, nil
	}

	filter := map[string]any{
		"id": articleID,
	}
	docs, err := Articles.Get(filter, nil)
	if err != nil {
		return article_models.ArticlePayload{}, false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger l'article depuis le stockage L2.",
			err,
		)
	}
	if len(docs) == 0 {
		return article_models.ArticlePayload{}, false, nil
	}

	var article article_models.ArticlePayload
	if err := pkg.ToStruct(docs[0], &article); err != nil {
		return article_models.ArticlePayload{}, false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de désérialiser l'article depuis le stockage L2.",
			err,
		)
	}
	article.Normalize()
	return article, true, nil
}
