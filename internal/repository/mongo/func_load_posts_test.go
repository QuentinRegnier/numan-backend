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

func TestMongoLoadPosts(t *testing.T) {
	defer test.ResetCollections()

	ctx := context.Background()

	// 1. Posts is nil
	Posts = nil
	posts, err := MongoLoadPosts(ctx, []int64{1, 2})
	assert.Nil(t, posts)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. Empty input
	mt.Run("Empty IDs returns non-nil slice", func(mt *mtest.T) {
		Posts = test.NewMockCollection(mt, "posts", nil)
		posts, err := MongoLoadPosts(ctx, []int64{})
		require.NoError(mt, err)
		assert.NotNil(mt, posts)
		assert.Empty(mt, posts)
	})

	// 3. DB error
	mt.Run("DB error", func(mt *mtest.T) {
		Posts = test.NewMockCollection(mt, "posts", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		posts, err := MongoLoadPosts(ctx, []int64{10})
		assert.Nil(mt, posts)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 4. Deserialization error
	mt.Run("Deserialization error", func(mt *mtest.T) {
		Posts = test.NewMockCollection(mt, "posts", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.posts", mtest.FirstBatch, bson.D{
			{Key: "id", Value: "invalid_id_not_int64"},
		}))

		posts, err := MongoLoadPosts(ctx, []int64{10})
		assert.Nil(mt, posts)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 5. Success
	mt.Run("Success", func(mt *mtest.T) {
		Posts = test.NewMockCollection(mt, "posts", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.posts", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(10)},
			{Key: "user_id", Value: int64(42)},
			{Key: "content", Value: "Hello world post!"},
		}))

		posts, err := MongoLoadPosts(ctx, []int64{10})
		require.NoError(mt, err)
		require.Len(mt, posts, 1)
		assert.Equal(mt, int64(10), posts[0].ID)
		assert.Equal(mt, "Hello world post!", posts[0].Content)
	})
}
