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

func TestMongoGetFirebaseInstallationIDs(t *testing.T) {
	defer test.ResetCollections()

	ctx := context.Background()

	// 1. mongo.Sessions is nil
	mongo.Sessions = nil
	tokens, err := mongo.MongoGetFirebaseInstallationIDs(ctx, 123)
	assert.Nil(t, tokens)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. DB error
	mt.Run("DB error", func(mt *mtest.T) {
		mongo.Sessions = test.NewMockCollection(mt, "sessions", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		tokens, err := mongo.MongoGetFirebaseInstallationIDs(ctx, 123)
		assert.Nil(mt, tokens)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 3. Empty result returns non-nil empty slice
	mt.Run("Empty result", func(mt *mtest.T) {
		mongo.Sessions = test.NewMockCollection(mt, "sessions", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.sessions", mtest.FirstBatch))

		tokens, err := mongo.MongoGetFirebaseInstallationIDs(ctx, 456)
		require.NoError(mt, err)
		assert.NotNil(mt, tokens)
		assert.Empty(mt, tokens)
	})

	// 4. Success with tokens
	mt.Run("Success with tokens", func(mt *mtest.T) {
		mongo.Sessions = test.NewMockCollection(mt, "sessions", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.sessions", mtest.FirstBatch,
			bson.D{{Key: "firebase_installation_id", Value: "token-abc"}},
			bson.D{{Key: "firebase_installation_id", Value: "token-def"}},
			bson.D{{Key: "firebase_installation_id", Value: ""}}, // empty token ignored
		))

		tokens, err := mongo.MongoGetFirebaseInstallationIDs(ctx, 789)
		require.NoError(mt, err)
		assert.Equal(mt, []string{"token-abc", "token-def"}, tokens)
	})
}
