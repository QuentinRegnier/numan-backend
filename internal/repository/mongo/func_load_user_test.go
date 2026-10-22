package mongo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoLoadUser(t *testing.T) {
	defer resetCollections()()

	ctx := context.Background()

	// 1. Users is nil
	Users = nil
	u, err := MongoLoadUser(ctx, 10, "", "", "")
	assert.Empty(t, u.ID)
	assertDatabaseError(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. No search criteria provided
	mt.Run("No search criteria provided", func(mt *mtest.T) {
		Users = newMockCollection(mt, "users", nil)
		u, err := MongoLoadUser(ctx, -1, "", "", "")
		assert.Empty(mt, u.ID)
		assertDatabaseError(mt, err)
	})

	// 3. DB error
	mt.Run("DB error", func(mt *mtest.T) {
		Users = newMockCollection(mt, "users", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		u, err := MongoLoadUser(ctx, 10, "", "", "")
		assert.Empty(mt, u.ID)
		assertDatabaseError(mt, err)
	})

	// 4. Not found
	mt.Run("Not found", func(mt *mtest.T) {
		Users = newMockCollection(mt, "users", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.users", mtest.FirstBatch))

		u, err := MongoLoadUser(ctx, 10, "", "", "")
		assert.Empty(mt, u.ID)
		assertNotFoundError(mt, err, "USER_NOT_FOUND")
	})

	// 5. Deserialization error
	mt.Run("Deserialization error", func(mt *mtest.T) {
		Users = newMockCollection(mt, "users", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.users", mtest.FirstBatch, bson.D{
			{Key: "id", Value: "invalid_id_not_int64"},
		}))

		u, err := MongoLoadUser(ctx, 10, "", "", "")
		assert.Empty(mt, u.ID)
		assertDatabaseError(mt, err)
	})

	// 6. Success
	mt.Run("Success with ID", func(mt *mtest.T) {
		Users = newMockCollection(mt, "users", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.users", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(10)},
			{Key: "username", Value: "alice"},
			{Key: "email", Value: "alice@example.com"},
		}))

		u, err := MongoLoadUser(ctx, 10, "", "", "")
		require.NoError(mt, err)
		assert.Equal(mt, int64(10), u.ID)
		assert.Equal(mt, "alice", u.Username)
	})

	mt.Run("Success with Email", func(mt *mtest.T) {
		Users = newMockCollection(mt, "users", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.users", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(20)},
			{Key: "username", Value: "bob"},
			{Key: "email", Value: "bob@example.com"},
		}))

		u, err := MongoLoadUser(ctx, -1, "", "bob@example.com", "")
		require.NoError(mt, err)
		assert.Equal(mt, int64(20), u.ID)
		assert.Equal(mt, "bob", u.Username)
	})
}
