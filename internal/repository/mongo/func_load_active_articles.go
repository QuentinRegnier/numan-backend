package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoLoadActiveArticles récupère tous les articles actifs (visibility >= 0) depuis MongoDB L2, triés par created_at DESC.
func MongoLoadActiveArticles(ctx context.Context) ([]article_models.ArticlePayload, error) {
	if Articles == nil || Articles.DB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Articles' is not initialized"),
		)
	}

	filter := bson.M{
		"visibility": bson.M{"$gte": variables.ArticleVisibilityPublished},
	}
	findOpts := options.Find().SetSort(bson.D{
		{Key: "created_at", Value: -1},
		{Key: "id", Value: -1},
	})

	cursor, err := Articles.DB.Collection(Articles.Name).Find(ctx, filter, findOpts)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les articles actifs depuis le stockage L2.",
			err,
		)
	}
	defer cursor.Close(ctx)

	var docs []map[string]any
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'extraire les articles actifs depuis le stockage L2.",
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
				"Impossible de convertir l'article actif depuis le stockage L2.",
				err,
			)
		}
		a.Normalize()
		articles = append(articles, a)
	}
	return articles, nil
}
