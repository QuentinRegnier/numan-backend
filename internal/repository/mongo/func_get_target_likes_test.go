package mongo_test

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoGetTargetLikes(t *testing.T) {
	defer test.ResetCollections()
	ctx := context.Background()

	mongo.Likes = nil
	likes, err := mongo.MongoGetTargetLikes(ctx, 1, 200, 100)
	assert.Nil(t, likes)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("DB error", func(mt *mtest.T) {
		mongo.Likes = test.NewMockCollection(mt, "likes", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		likes, err := mongo.MongoGetTargetLikes(ctx, 1, 200, 100)
		assert.Nil(mt, likes)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	mt.Run("Empty result", func(mt *mtest.T) {
		mongo.Likes = test.NewMockCollection(mt, "likes", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.likes", mtest.FirstBatch))

		likes, err := mongo.MongoGetTargetLikes(ctx, 0, 100, 100)
		require.NoError(mt, err)
		assert.Empty(mt, likes)
	})

	mt.Run("Success for comment likes", func(mt *mtest.T) {
		mongo.Likes = test.NewMockCollection(mt, "likes", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.likes", mtest.FirstBatch,
			bson.D{{Key: "user_id", Value: float64(11)}},
			bson.D{{Key: "user_id", Value: int64(22)}},
			bson.D{{Key: "user_id", Value: int32(33)}},
		))

		likes, err := mongo.MongoGetTargetLikes(ctx, 1, 200, 100)
		require.NoError(mt, err)
		assert.Equal(mt, []int64{11, 22, 33}, likes)
	})
}

func TestMongoHasUserLiked(t *testing.T) {
	defer test.ResetCollections()
	ctx := context.Background()

	mongo.Likes = nil
	found, err := mongo.MongoHasUserLiked(ctx, 0, 100, 11)
	assert.False(t, found)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("DB error", func(mt *mtest.T) {
		mongo.Likes = test.NewMockCollection(mt, "likes", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		found, err := mongo.MongoHasUserLiked(ctx, 0, 100, 11)
		assert.False(mt, found)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	mt.Run("Not found", func(mt *mtest.T) {
		mongo.Likes = test.NewMockCollection(mt, "likes", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.likes", mtest.FirstBatch))

		found, err := mongo.MongoHasUserLiked(ctx, 0, 100, 11)
		require.NoError(mt, err)
		assert.False(mt, found)
	})

	mt.Run("Found", func(mt *mtest.T) {
		mongo.Likes = test.NewMockCollection(mt, "likes", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.likes", mtest.FirstBatch,
			bson.D{{Key: "user_id", Value: int64(11)}},
		))

		found, err := mongo.MongoHasUserLiked(ctx, 1, 200, 11)
		require.NoError(mt, err)
		assert.True(mt, found)
	})
}
