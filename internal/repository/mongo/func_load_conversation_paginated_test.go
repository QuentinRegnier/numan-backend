package mongo

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoLoadConversationPaginated(t *testing.T) {
	defer test.ResetCollections()

	// 1. Members or Conversations is nil
	Members = nil
	Conversations = nil
	results, err := MongoLoadConversationPaginated(10, 10, 0)
	assert.Nil(t, results)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. Aggregate DB error
	mt.Run("Aggregate DB error", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		Conversations = test.NewMockCollection(mt, "conversations", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		results, err := MongoLoadConversationPaginated(10, 10, 0)
		assert.Nil(mt, results)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 3. Empty result
	mt.Run("Empty result", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		Conversations = test.NewMockCollection(mt, "conversations", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.members", mtest.FirstBatch))

		results, err := MongoLoadConversationPaginated(10, 10, 0)
		require.NoError(mt, err)
		assert.NotNil(mt, results)
		assert.Empty(mt, results)
	})

	// 4. Invalid conversation_meta field
	mt.Run("Invalid conversation_meta field", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		Conversations = test.NewMockCollection(mt, "conversations", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.members", mtest.FirstBatch, bson.D{
			{Key: "conversation_meta", Value: "not_a_bson_m"},
		}))

		results, err := MongoLoadConversationPaginated(10, 10, 0)
		assert.Nil(mt, results)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 5. Deserialization error
	mt.Run("Deserialization error", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		Conversations = test.NewMockCollection(mt, "conversations", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.members", mtest.FirstBatch, bson.D{
			{Key: "conversation_meta", Value: bson.M{"id": "not_an_int"}},
		}))

		results, err := MongoLoadConversationPaginated(10, 10, 0)
		assert.Nil(mt, results)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 6. Success
	mt.Run("Success", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		Conversations = test.NewMockCollection(mt, "conversations", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.members", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(1)},
			{Key: "conversation_id", Value: int64(100)},
			{Key: "user_id", Value: int64(10)},
			{Key: "conversation_meta", Value: bson.M{
				"id":    int64(100),
				"title": "General",
			}},
		}))

		results, err := MongoLoadConversationPaginated(10, 10, 0)
		require.NoError(mt, err)
		require.Len(mt, results, 1)
		assert.Equal(mt, int64(100), results[0].Conversation.ID)
		assert.Equal(mt, "General", results[0].Conversation.Title)
		assert.Equal(mt, int64(1), results[0].Member.ID)
	})
}
