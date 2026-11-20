package mongo_test

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoUpsertArticle(t *testing.T) {
	defer test.ResetCollections()()
	ctx := context.Background()

	t.Run("nil_collection", func(t *testing.T) {
		mongo.Articles = nil
		err := mongo.MongoUpsertArticle(ctx, article_models.ArticlePayload{ID: 1})
		test.AssertDatabaseError_Mongo(t, err)
	})

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("success_and_error", func(mt *mtest.T) {
		mongo.Articles = test.NewMockCollection(mt, "content.articles", nil)

		mt.AddMockResponses(mtest.CreateSuccessResponse())
		err := mongo.MongoUpsertArticle(ctx, article_models.ArticlePayload{
			ID:    103,
			Title: "Upserted",
		})
		require.NoError(mt, err)

		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "fail"}))
		errDB := mongo.MongoUpsertArticle(ctx, article_models.ArticlePayload{ID: 103})
		test.AssertDatabaseError_Mongo(mt, errDB)
	})
}
