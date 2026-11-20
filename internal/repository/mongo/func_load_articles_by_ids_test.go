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

func TestMongoLoadArticlesByIDs(t *testing.T) {
	defer test.ResetCollections()()
	ctx := context.Background()

	t.Run("nil_collection", func(t *testing.T) {
		mongo.Articles = nil
		_, err := mongo.MongoLoadArticlesByIDs(ctx, []int64{1})
		test.AssertDatabaseError_Mongo(t, err)
	})

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("empty_success_and_error", func(mt *mtest.T) {
		mongo.Articles = test.NewMockCollection(mt, "content.articles", nil)

		empty, err := mongo.MongoLoadArticlesByIDs(ctx, nil)
		require.NoError(mt, err)
		assert.Empty(mt, empty)

		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.content.articles", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(101)},
			{Key: "title", Value: "Guide"},
		}))
		list, err := mongo.MongoLoadArticlesByIDs(ctx, []int64{101})
		require.NoError(mt, err)
		require.Len(mt, list, 1)
		assert.Equal(mt, int64(101), list[0].ID)

		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "fail"}))
		_, errDB := mongo.MongoLoadArticlesByIDs(ctx, []int64{101})
		test.AssertDatabaseError_Mongo(mt, errDB)
	})
}
