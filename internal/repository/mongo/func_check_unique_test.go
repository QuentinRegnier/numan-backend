package mongo

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoCheckUnique(t *testing.T) {
	defer test.ResetCollections()

	ctx := context.Background()

	// 1. Uninitialized collection
	Users = nil
	exists, err := MongoCheckUnique(ctx, redis.EntityUser, "email", "test@test.com")
	assert.False(t, exists)
	test.AssertDatabaseError_Mongo(t, err)

	// 2. Unsupported entity
	exists, err = MongoCheckUnique(ctx, redis.EntityType("invalid"), "email", "test@test.com")
	assert.False(t, exists)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("DB error", func(mt *mtest.T) {
		Users = test.NewMockCollection(mt, "users", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "query failure"}))

		exists, err := MongoCheckUnique(ctx, redis.EntityUser, "username", "john")
		assert.False(mt, exists)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	mt.Run("Found value (exists = true)", func(mt *mtest.T) {
		Users = test.NewMockCollection(mt, "users", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.users", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(100)},
		}))

		exists, err := MongoCheckUnique(ctx, redis.EntityUser, "username", "john")
		require.NoError(mt, err)
		assert.True(mt, exists)
	})

	mt.Run("Not found value (exists = false)", func(mt *mtest.T) {
		Users = test.NewMockCollection(mt, "users", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.users", mtest.FirstBatch))

		exists, err := MongoCheckUnique(ctx, redis.EntityUser, "username", "nobody")
		require.NoError(mt, err)
		assert.False(mt, exists)
	})
}
