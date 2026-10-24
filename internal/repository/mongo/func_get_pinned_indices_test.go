package mongo

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoGetPinnedIndices(t *testing.T) {
	defer test.ResetCollections()

	// 1. Members is nil
	Members = nil
	indices, err := MongoGetPinnedIndices(123)
	assert.Nil(t, indices)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. DB error
	mt.Run("DB error", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		indices, err := MongoGetPinnedIndices(123)
		assert.Nil(mt, indices)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 3. Empty result
	mt.Run("Empty result", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.members", mtest.FirstBatch))

		indices, err := MongoGetPinnedIndices(123)
		require.NoError(mt, err)
		assert.NotNil(mt, indices)
		assert.Empty(mt, indices)
	})

	// 4. Deserialization error
	mt.Run("Deserialization error", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.members", mtest.FirstBatch, bson.D{
			{Key: "settings", Value: "invalid_not_a_map"},
		}))

		indices, err := MongoGetPinnedIndices(123)
		assert.Nil(mt, indices)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 5. Success
	mt.Run("Success with pinned indices", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.members", mtest.FirstBatch,
			bson.D{{Key: "id", Value: int64(1)}, {Key: "settings", Value: bson.M{"pinned": 0}}},
			bson.D{{Key: "id", Value: int64(2)}, {Key: "settings", Value: bson.M{"pinned": 2}}},
			bson.D{{Key: "id", Value: int64(3)}, {Key: "settings", Value: bson.M{"pinned": -1}}}, // unpinned
		))

		indices, err := MongoGetPinnedIndices(123)
		require.NoError(mt, err)
		assert.Equal(mt, []int{0, 2}, indices)
	})
}
