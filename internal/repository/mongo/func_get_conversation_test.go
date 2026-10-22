package mongo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoGetConversation(t *testing.T) {
	defer resetCollections()()

	ctx := context.Background()

	// 1. Conversations is nil
	Conversations = nil
	conv, err := MongoGetConversation(ctx, 123)
	assert.Empty(t, conv.ID)
	assertDatabaseError(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. DB query error
	mt.Run("DB query error", func(mt *mtest.T) {
		Conversations = newMockCollection(mt, "conversations", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "mongo down"}))

		c, err := MongoGetConversation(ctx, 123)
		assert.Empty(mt, c.ID)
		assertDatabaseError(mt, err)
	})

	// 3. Not found (len(docs) == 0)
	mt.Run("Query succeeds but returns no documents", func(mt *mtest.T) {
		Conversations = newMockCollection(mt, "conversations", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.conversations", mtest.FirstBatch))

		c, err := MongoGetConversation(ctx, 456)
		assert.Empty(mt, c.ID)
		assertNotFoundError(mt, err, "CONV_NOT_FOUND")
	})

	// 4. Deserialization error
	mt.Run("BSON deserialization fails", func(mt *mtest.T) {
		Conversations = newMockCollection(mt, "conversations", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.conversations", mtest.FirstBatch, bson.D{
			{Key: "id", Value: "invalid_not_int64"},
		}))

		c, err := MongoGetConversation(ctx, 789)
		assert.Empty(mt, c.ID)
		assertDatabaseError(mt, err)
	})

	// 5. Success
	mt.Run("Query succeeds and returns valid document", func(mt *mtest.T) {
		Conversations = newMockCollection(mt, "conversations", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.conversations", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(999)},
			{Key: "title", Value: "Test Conversation"},
		}))

		c, err := MongoGetConversation(ctx, 999)
		require.NoError(mt, err)
		assert.Equal(mt, int64(999), c.ID)
		assert.Equal(mt, "Test Conversation", c.Title)
	})
}
