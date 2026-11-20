package mongo_test

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoLoadActiveArticles(t *testing.T) {
	defer test.ResetCollections()()
	ctx := context.Background()

	t.Run("nil_collection", func(t *testing.T) {
		mongo.Articles = nil
		_, err := mongo.MongoLoadActiveArticles(ctx)
		test.AssertDatabaseError_Mongo(t, err)
	})

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("success_and_error", func(mt *mtest.T) {
		mongo.Articles = test.NewMockCollection(mt, "content.articles", nil)

		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.content.articles", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(102)},
			{Key: "title", Value: "Active Guide"},
			{Key: "visibility", Value: int32(0)},
		}))
		active, err := mongo.MongoLoadActiveArticles(ctx)
		require.NoError(mt, err)
		require.Len(mt, active, 1)
		assert.Equal(mt, int64(102), active[0].ID)

		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "fail"}))
		_, errDB := mongo.MongoLoadActiveArticles(ctx)
		test.AssertDatabaseError_Mongo(mt, errDB)
	})
}
