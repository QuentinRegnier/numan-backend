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

func TestMongoGetMessageReactionsPaginated(t *testing.T) {
	defer test.ResetCollections()

	ctx := context.Background()

	// 1. MessageReactions is nil
	MessageReactions = nil
	reactions, err := MongoGetMessageReactionsPaginated(ctx, 123, 10, 0)
	assert.Nil(t, reactions)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. DB error
	mt.Run("DB error", func(mt *mtest.T) {
		MessageReactions = test.NewMockCollection(mt, "message_reactions", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		reactions, err := MongoGetMessageReactionsPaginated(ctx, 123, 10, 0)
		assert.Nil(mt, reactions)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 3. Empty result returns non-nil slice
	mt.Run("Empty result", func(mt *mtest.T) {
		MessageReactions = test.NewMockCollection(mt, "message_reactions", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.message_reactions", mtest.FirstBatch))

		reactions, err := MongoGetMessageReactionsPaginated(ctx, 123, 10, 0)
		require.NoError(mt, err)
		assert.NotNil(mt, reactions)
		assert.Empty(mt, reactions)
	})

	// 4. Deserialization error
	mt.Run("Deserialization error", func(mt *mtest.T) {
		MessageReactions = test.NewMockCollection(mt, "message_reactions", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.message_reactions", mtest.FirstBatch, bson.D{
			{Key: "id", Value: "invalid_id_not_int64"},
		}))

		reactions, err := MongoGetMessageReactionsPaginated(ctx, 123, 10, 0)
		assert.Nil(mt, reactions)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 5. Success
	mt.Run("Success", func(mt *mtest.T) {
		MessageReactions = test.NewMockCollection(mt, "message_reactions", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.message_reactions", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(1)},
			{Key: "message_id", Value: int64(123)},
			{Key: "user_id", Value: int64(456)},
			{Key: "reaction", Value: "👍"},
			{Key: "created_at", Value: int64(1000)},
		}))

		reactions, err := MongoGetMessageReactionsPaginated(ctx, 123, 10, 0)
		require.NoError(mt, err)
		require.Len(mt, reactions, 1)
		assert.Equal(mt, int64(1), reactions[0].ID)
		assert.Equal(mt, "👍", reactions[0].Reaction)
	})
}
