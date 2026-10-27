package mongo_test

import (
	"context"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoLoadCommentsPaginated(t *testing.T) {
	defer test.ResetCollections()

	ctx := context.Background()

	// 1. mongo.Comments is nil
	mongo.Comments = nil
	comments, err := mongo.MongoLoadCommentsPaginated(ctx, 100, 0, 10)
	assert.Nil(t, comments)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. DB error
	mt.Run("DB error", func(mt *mtest.T) {
		mongo.Comments = test.NewMockCollection(mt, "comments", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		comments, err := mongo.MongoLoadCommentsPaginated(ctx, 100, 0, 10)
		assert.Nil(mt, comments)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 3. Empty result
	mt.Run("Empty result", func(mt *mtest.T) {
		mongo.Comments = test.NewMockCollection(mt, "comments", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.comments", mtest.FirstBatch))

		comments, err := mongo.MongoLoadCommentsPaginated(ctx, 100, 0, 10)
		require.NoError(mt, err)
		assert.NotNil(mt, comments)
		assert.Empty(mt, comments)
	})

	// 4. Deserialization error
	mt.Run("Deserialization error", func(mt *mtest.T) {
		mongo.Comments = test.NewMockCollection(mt, "comments", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.comments", mtest.FirstBatch, bson.D{
			{Key: "id", Value: "not_an_int64"},
		}))

		comments, err := mongo.MongoLoadCommentsPaginated(ctx, 100, 0, 10)
		assert.Nil(mt, comments)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 5. Success
	mt.Run("Success", func(mt *mtest.T) {
		mongo.Comments = test.NewMockCollection(mt, "comments", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.comments", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(201)},
			{Key: "post_id", Value: int64(100)},
			{Key: "content", Value: "Awesome!"},
		}))

		comments, err := mongo.MongoLoadCommentsPaginated(ctx, 100, 0, 10)
		require.NoError(mt, err)
		require.Len(mt, comments, 1)
		assert.Equal(mt, int64(201), comments[0].ID)
		assert.Equal(mt, "Awesome!", comments[0].Content)
	})
}
