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

func TestMongoLoadMessagesByIDs(t *testing.T) {
	defer test.ResetCollections()

	ctx := context.Background()

	// 1. Messages is nil
	Messages = nil
	messages, err := MongoLoadMessagesByIDs(ctx, []int64{1, 2})
	assert.Nil(t, messages)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. Empty input
	mt.Run("Empty messageIDs returns non-nil slice", func(mt *mtest.T) {
		Messages = test.NewMockCollection(mt, "messages", nil)
		messages, err := MongoLoadMessagesByIDs(ctx, []int64{})
		require.NoError(mt, err)
		assert.NotNil(mt, messages)
		assert.Empty(mt, messages)
	})

	// 3. DB error
	mt.Run("DB error", func(mt *mtest.T) {
		Messages = test.NewMockCollection(mt, "messages", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		messages, err := MongoLoadMessagesByIDs(ctx, []int64{10})
		assert.Nil(mt, messages)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 4. Deserialization error
	mt.Run("Deserialization error", func(mt *mtest.T) {
		Messages = test.NewMockCollection(mt, "messages", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.messages", mtest.FirstBatch, bson.D{
			{Key: "id", Value: "invalid_id_not_int64"},
		}))

		messages, err := MongoLoadMessagesByIDs(ctx, []int64{10})
		assert.Nil(mt, messages)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 5. Success
	mt.Run("Success", func(mt *mtest.T) {
		Messages = test.NewMockCollection(mt, "messages", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.messages", mtest.FirstBatch,
			bson.D{
				{Key: "id", Value: int64(1)},
				{Key: "content", Value: "Hello!"},
				{Key: "message_type", Value: int32(0)},
				{Key: "allow_new_reactions", Value: true},
			},
			bson.D{
				{Key: "id", Value: int64(2)},
				{Key: "content", Value: "World!"},
				{Key: "message_type", Value: int32(0)},
				// allow_new_reactions missing -> evaluated via variables.ShouldAllowNewReactions
			},
		))

		messages, err := MongoLoadMessagesByIDs(ctx, []int64{1, 2})
		require.NoError(mt, err)
		require.Len(mt, messages, 2)
		assert.Equal(mt, int64(1), messages[0].ID)
		assert.True(mt, messages[0].AllowNewReactions)
		assert.Equal(mt, int64(2), messages[1].ID)
	})
}
