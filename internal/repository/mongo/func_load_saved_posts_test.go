package mongo

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoLoadSavedPosts(t *testing.T) {
	defer test.ResetCollections()

	ctx := context.Background()

	// 1. Saved is nil
	Saved = nil
	saveds, err := MongoLoadSavedPosts(ctx, 42, 10, 0)
	assert.Nil(t, saveds)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. DB error
	mt.Run("DB error", func(mt *mtest.T) {
		Saved = test.NewMockCollection(mt, "saved", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		saveds, err := MongoLoadSavedPosts(ctx, 42, 10, 0)
		assert.Nil(mt, saveds)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 3. Empty result
	mt.Run("Empty result", func(mt *mtest.T) {
		Saved = test.NewMockCollection(mt, "saved", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.saved", mtest.FirstBatch))

		saveds, err := MongoLoadSavedPosts(ctx, 42, 10, 0)
		require.NoError(mt, err)
		assert.NotNil(mt, saveds)
		assert.Empty(mt, saveds)
	})

	// 4. Deserialization error
	mt.Run("Deserialization error", func(mt *mtest.T) {
		Saved = test.NewMockCollection(mt, "saved", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.saved", mtest.FirstBatch, bson.D{
			{Key: "id", Value: "invalid_id_not_int64"},
		}))

		saveds, err := MongoLoadSavedPosts(ctx, 42, 10, 0)
		assert.Nil(mt, saveds)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 5. Success
	mt.Run("Success", func(mt *mtest.T) {
		Saved = test.NewMockCollection(mt, "saved", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.saved", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(1)},
			{Key: "user_id", Value: int64(42)},
			{Key: "post_id", Value: int64(100)},
		}))

		saveds, err := MongoLoadSavedPosts(ctx, 42, 10, 0)
		require.NoError(mt, err)
		require.Len(mt, saveds, 1)
		assert.Equal(mt, int64(1), saveds[0].ID)
		assert.Equal(mt, int64(100), saveds[0].PostID)
	})
}
