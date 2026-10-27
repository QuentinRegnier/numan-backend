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

func TestMongoLoadComments(t *testing.T) {
	defer test.ResetCollections()

	ctx := context.Background()

	// 1. mongo.Comments is nil
	mongo.Comments = nil
	comments, err := mongo.MongoLoadComments(ctx, []int64{1, 2})
	assert.Nil(t, comments)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. Empty input
	mt.Run("Empty commentIDs returns non-nil slice", func(mt *mtest.T) {
		mongo.Comments = test.NewMockCollection(mt, "comments", nil)
		comments, err := mongo.MongoLoadComments(ctx, []int64{})
		require.NoError(mt, err)
		assert.NotNil(mt, comments)
		assert.Empty(mt, comments)
	})

	// 3. DB error
	mt.Run("DB error", func(mt *mtest.T) {
		mongo.Comments = test.NewMockCollection(mt, "comments", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		comments, err := mongo.MongoLoadComments(ctx, []int64{10})
		assert.Nil(mt, comments)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 4. Deserialization error
	mt.Run("Deserialization error", func(mt *mtest.T) {
		mongo.Comments = test.NewMockCollection(mt, "comments", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.comments", mtest.FirstBatch, bson.D{
			{Key: "id", Value: "invalid_id_type"},
		}))

		comments, err := mongo.MongoLoadComments(ctx, []int64{10})
		assert.Nil(mt, comments)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 5. Success
	mt.Run("Success", func(mt *mtest.T) {
		mongo.Comments = test.NewMockCollection(mt, "comments", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.comments", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(10)},
			{Key: "post_id", Value: int64(100)},
			{Key: "content", Value: "Great post!"},
		}))

		comments, err := mongo.MongoLoadComments(ctx, []int64{10})
		require.NoError(mt, err)
		require.Len(mt, comments, 1)
		assert.Equal(mt, int64(10), comments[0].ID)
		assert.Equal(mt, "Great post!", comments[0].Content)
	})
}
