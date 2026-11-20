package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"go.mongodb.org/mongo-driver/bson"
	libMongo "go.mongodb.org/mongo-driver/mongo"
)

// MongoUpsertArticle enregistre ou met à jour un article dans MongoDB L2 de manière idempotente.
func MongoUpsertArticle(ctx context.Context, article article_models.ArticlePayload) error {
	if Articles == nil || Articles.DB == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Articles' is not initialized"),
		)
	}
	article.Normalize()
	doc, err := pkg.ToMap(article)
	if err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de sérialiser l'article pour MongoDB L2.",
			err,
		)
	}
	doc["last_use"] = domain.NowTime()

	model := libMongo.NewUpdateOneModel().
		SetFilter(bson.M{"id": article.ID}).
		SetUpdate(bson.M{"$set": doc}).
		SetUpsert(true)

	_, err = Articles.DB.Collection(Articles.Name).BulkWrite(ctx, []libMongo.WriteModel{model})
	if err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de persister l'article dans MongoDB L2.",
			err,
		)
	}
	return nil
}
