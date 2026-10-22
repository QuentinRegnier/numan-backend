package mongo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoLoadSession(t *testing.T) {
	defer resetCollections()()

	ctx := context.Background()

	// 1. Sessions is nil
	Sessions = nil
	s, err := MongoLoadSession(ctx, 10, "firebase-123", "", "")
	assert.Empty(t, s.ID)
	assertDatabaseError(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. No search criteria provided
	mt.Run("No search criteria provided", func(mt *mtest.T) {
		Sessions = newMockCollection(mt, "sessions", nil)
		s, err := MongoLoadSession(ctx, -1, "", "", "")
		assert.Empty(mt, s.ID)
		assertDatabaseError(mt, err)
	})

	// 3. DB error
	mt.Run("DB error", func(mt *mtest.T) {
		Sessions = newMockCollection(mt, "sessions", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		s, err := MongoLoadSession(ctx, 10, "", "", "")
		assert.Empty(mt, s.ID)
		assertDatabaseError(mt, err)
	})

	// 4. Not found
	mt.Run("Not found", func(mt *mtest.T) {
		Sessions = newMockCollection(mt, "sessions", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.sessions", mtest.FirstBatch))

		s, err := MongoLoadSession(ctx, 10, "", "", "")
		assert.Empty(mt, s.ID)
		assertNotFoundError(mt, err, "SESSION_NOT_FOUND")
	})

	// 5. Deserialization error
	mt.Run("Deserialization error", func(mt *mtest.T) {
		Sessions = newMockCollection(mt, "sessions", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.sessions", mtest.FirstBatch, bson.D{
			{Key: "id", Value: "invalid_id_not_int64"},
		}))

		s, err := MongoLoadSession(ctx, 10, "", "", "")
		assert.Empty(mt, s.ID)
		assertDatabaseError(mt, err)
	})

	// 6. Success
	mt.Run("Success", func(mt *mtest.T) {
		Sessions = newMockCollection(mt, "sessions", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.sessions", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(101)},
			{Key: "user_id", Value: int64(10)},
			{Key: "firebase_installation_id", Value: "firebase-xyz"},
		}))

		s, err := MongoLoadSession(ctx, 10, "", "", "")
		require.NoError(mt, err)
		assert.Equal(mt, int64(101), s.ID)
		assert.Equal(mt, int64(10), s.UserID)
		assert.Equal(mt, "firebase-xyz", s.FirebaseInstallationID)
	})
}
