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

func TestMongoGetArticleByID(t *testing.T) {
	defer test.ResetCollections()()
	ctx := context.Background()

	t.Run("nil_collection", func(t *testing.T) {
		mongo.Articles = nil
		_, _, err := mongo.MongoGetArticleByID(ctx, 1)
		test.AssertDatabaseError_Mongo(t, err)
	})

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("zero_found_not_found_and_error", func(mt *mtest.T) {
		mongo.Articles = test.NewMockCollection(mt, "content.articles", nil)

		artZero, foundZero, errZero := mongo.MongoGetArticleByID(ctx, 0)
		require.NoError(mt, errZero)
		assert.False(mt, foundZero)
		assert.Equal(mt, int64(0), artZero.ID)

		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.content.articles", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(101)},
			{Key: "title", Value: "Guide"},
			{Key: "summary", Value: "Résumé"},
			{Key: "cover_media_id", Value: int64(201)},
			{Key: "markdown_media_id", Value: int64(301)},
			{Key: "attachment_media_ids", Value: bson.A{int64(401)}},
			{Key: "visibility", Value: int32(0)},
			{Key: "created_at", Value: int64(1700000000000)},
			{Key: "updated_at", Value: int64(1700000000000)},
		}))
		art, found, err := mongo.MongoGetArticleByID(ctx, 101)
		require.NoError(mt, err)
		assert.True(mt, found)
		assert.Equal(mt, int64(101), art.ID)
		assert.Equal(mt, "Guide", art.Title)
		assert.Equal(mt, []int64{401}, art.AttachmentMediaIDs)

		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.content.articles", mtest.FirstBatch))
		_, foundMissing, errMissing := mongo.MongoGetArticleByID(ctx, 999)
		require.NoError(mt, errMissing)
		assert.False(mt, foundMissing)

		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "fail"}))
		_, _, errDB := mongo.MongoGetArticleByID(ctx, 101)
		test.AssertDatabaseError_Mongo(mt, errDB)
	})
}
