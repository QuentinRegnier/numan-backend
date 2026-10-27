package mongo_test

import (
	"context"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoLoadSession(t *testing.T) {
	defer test.ResetCollections()

	ctx := context.Background()

	// 1. mongo.Sessions is nil
	mongo.Sessions = nil
	s, err := mongo.MongoLoadSession(ctx, 10, "firebase-123", "", "")
	assert.Empty(t, s.ID)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. No search criteria provided
	mt.Run("No search criteria provided", func(mt *mtest.T) {
		mongo.Sessions = test.NewMockCollection(mt, "sessions", nil)
		s, err := mongo.MongoLoadSession(ctx, -1, "", "", "")
		assert.Empty(mt, s.ID)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 3. DB error
	mt.Run("DB error", func(mt *mtest.T) {
		mongo.Sessions = test.NewMockCollection(mt, "sessions", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		s, err := mongo.MongoLoadSession(ctx, 10, "", "", "")
		assert.Empty(mt, s.ID)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 4. Not found
	mt.Run("Not found", func(mt *mtest.T) {
		mongo.Sessions = test.NewMockCollection(mt, "sessions", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.sessions", mtest.FirstBatch))

		s, err := mongo.MongoLoadSession(ctx, 10, "", "", "")
		assert.Empty(mt, s.ID)
		test.AssertNotFoundError_Mongo(mt, err, "SESSION_NOT_FOUND")
	})

	// 5. Deserialization error
	mt.Run("Deserialization error", func(mt *mtest.T) {
		mongo.Sessions = test.NewMockCollection(mt, "sessions", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.sessions", mtest.FirstBatch, bson.D{
			{Key: "id", Value: "invalid_id_not_int64"},
		}))

		s, err := mongo.MongoLoadSession(ctx, 10, "", "", "")
		assert.Empty(mt, s.ID)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 6. Success
	mt.Run("Success", func(mt *mtest.T) {
		mongo.Sessions = test.NewMockCollection(mt, "sessions", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.sessions", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(101)},
			{Key: "user_id", Value: int64(10)},
			{Key: "firebase_installation_id", Value: "firebase-xyz"},
		}))

		s, err := mongo.MongoLoadSession(ctx, 10, "", "", "")
		require.NoError(mt, err)
		assert.Equal(mt, int64(101), s.ID)
		assert.Equal(mt, int64(10), s.UserID)
		assert.Equal(mt, "firebase-xyz", s.FirebaseInstallationID)
	})
}
