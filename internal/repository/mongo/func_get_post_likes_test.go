package mongo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoGetPostLikes(t *testing.T) {
	defer resetCollections()()

	ctx := context.Background()

	// 1. Likes is nil
	Likes = nil
	likes, err := MongoGetPostLikes(ctx, 123, 10, 0)
	assert.Nil(t, likes)
	assertDatabaseError(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. DB error
	mt.Run("DB error", func(mt *mtest.T) {
		Likes = newMockCollection(mt, "likes", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		likes, err := MongoGetPostLikes(ctx, 123, 10, 0)
		assert.Nil(mt, likes)
		assertDatabaseError(mt, err)
	})

	// 3. Empty result
	mt.Run("Empty result", func(mt *mtest.T) {
		Likes = newMockCollection(mt, "likes", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.likes", mtest.FirstBatch))

		likes, err := MongoGetPostLikes(ctx, 123, 10, 0)
		require.NoError(mt, err)
		assert.NotNil(mt, likes)
		assert.Empty(mt, likes)
	})

	// 4. Success with various user ID types
	mt.Run("Success with user IDs", func(mt *mtest.T) {
		Likes = newMockCollection(mt, "likes", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.likes", mtest.FirstBatch,
			bson.D{{Key: "user_id", Value: float64(101)}},
			bson.D{{Key: "user_id", Value: int64(102)}},
			bson.D{{Key: "user_id", Value: int32(103)}},
		))

		likes, err := MongoGetPostLikes(ctx, 123, 10, 0)
		require.NoError(mt, err)
		assert.Equal(mt, []int64{101, 102, 103}, likes)
	})
}
