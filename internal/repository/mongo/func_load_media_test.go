package mongo_test

import (
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoLoadMedia(t *testing.T) {
	defer test.ResetCollections()

	// 1. mongo.Media is nil
	mongo.Media = nil
	media, err := mongo.MongoLoadMedia([]int64{1, 2})
	assert.Nil(t, media)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. Empty input
	mt.Run("Empty mediaIDs returns non-nil slice", func(mt *mtest.T) {
		mongo.Media = test.NewMockCollection(mt, "media", nil)
		media, err := mongo.MongoLoadMedia([]int64{})
		require.NoError(mt, err)
		assert.NotNil(mt, media)
		assert.Empty(mt, media)
	})

	// 3. Find error
	mt.Run("Find error", func(mt *mtest.T) {
		mongo.Media = test.NewMockCollection(mt, "media", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "find failed"}))

		media, err := mongo.MongoLoadMedia([]int64{10})
		assert.Nil(mt, media)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 4. Empty result
	mt.Run("Empty result", func(mt *mtest.T) {
		mongo.Media = test.NewMockCollection(mt, "media", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.media", mtest.FirstBatch))

		media, err := mongo.MongoLoadMedia([]int64{10})
		require.NoError(mt, err)
		assert.NotNil(mt, media)
		assert.Empty(mt, media)
	})

	// 5. Success
	mt.Run("Success", func(mt *mtest.T) {
		mongo.Media = test.NewMockCollection(mt, "media", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.media", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(10)},
			{Key: "owner_id", Value: int64(42)},
			{Key: "storage_path", Value: "/path/to/media.jpg"},
			{Key: "visibility", Value: int32(1)},
		}))

		media, err := mongo.MongoLoadMedia([]int64{10})
		require.NoError(mt, err)
		require.Len(mt, media, 1)
		assert.Equal(mt, int64(10), media[0].ID)
		assert.Equal(mt, "/path/to/media.jpg", media[0].StoragePath)
		assert.Equal(mt, 1, int(media[0].Visibility))
	})
}
