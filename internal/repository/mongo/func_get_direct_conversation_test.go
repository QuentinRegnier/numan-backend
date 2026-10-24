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

func TestMongoGetDirectConversation(t *testing.T) {
	defer test.ResetCollections()

	ctx := context.Background()

	// 1. Uninitialized
	Members = nil
	Conversations = nil
	c, err := MongoGetDirectConversation(ctx, 1, 2)
	assert.Empty(t, c.ID)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. Aggregate DB error
	mt.Run("Aggregate DB error", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		Conversations = test.NewMockCollection(mt, "conversations", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "aggregate failed"}))

		c, err := MongoGetDirectConversation(ctx, 1, 2)
		assert.Empty(mt, c.ID)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 3. Not found (empty results)
	mt.Run("Not found (empty results)", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		Conversations = test.NewMockCollection(mt, "conversations", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.members", mtest.FirstBatch))

		c, err := MongoGetDirectConversation(ctx, 1, 2)
		assert.Empty(mt, c.ID)
		test.AssertNotFoundError_Mongo(mt, err, "CONV_NOT_FOUND")
	})

	// 4. Type assertion error for "conv"
	mt.Run("Invalid conv field type", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		Conversations = test.NewMockCollection(mt, "conversations", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.members", mtest.FirstBatch, bson.D{
			{Key: "conv", Value: "not_a_bson_m"},
		}))

		c, err := MongoGetDirectConversation(ctx, 1, 2)
		assert.Empty(mt, c.ID)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 5. Deserialization error
	mt.Run("Deserialization error", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		Conversations = test.NewMockCollection(mt, "conversations", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.members", mtest.FirstBatch, bson.D{
			{Key: "conv", Value: bson.M{"id": "not_an_int"}},
		}))

		c, err := MongoGetDirectConversation(ctx, 1, 2)
		assert.Empty(mt, c.ID)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 6. Success
	mt.Run("Success", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		Conversations = test.NewMockCollection(mt, "conversations", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.members", mtest.FirstBatch, bson.D{
			{Key: "conv", Value: bson.M{
				"id":    int64(42),
				"title": "Private Chat",
			}},
		}))

		c, err := MongoGetDirectConversation(ctx, 1, 2)
		require.NoError(mt, err)
		assert.Equal(mt, int64(42), c.ID)
		assert.Equal(mt, "Private Chat", c.Title)
	})
}
