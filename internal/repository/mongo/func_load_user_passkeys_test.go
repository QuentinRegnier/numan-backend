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

func TestMongoLoadUserPasskeys(t *testing.T) {
	defer test.ResetCollections()

	ctx := context.Background()

	// 1. Passkeys is nil
	Passkeys = nil
	passkeys, err := MongoLoadUserPasskeys(ctx, 42)
	assert.Nil(t, passkeys)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. DB error
	mt.Run("DB error", func(mt *mtest.T) {
		Passkeys = test.NewMockCollection(mt, "passkeys", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		passkeys, err := MongoLoadUserPasskeys(ctx, 42)
		assert.Nil(mt, passkeys)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 3. Empty result
	mt.Run("Empty result", func(mt *mtest.T) {
		Passkeys = test.NewMockCollection(mt, "passkeys", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.passkeys", mtest.FirstBatch))

		passkeys, err := MongoLoadUserPasskeys(ctx, 42)
		require.NoError(mt, err)
		assert.NotNil(mt, passkeys)
		assert.Empty(mt, passkeys)
	})

	// 4. Deserialization error
	mt.Run("Deserialization error", func(mt *mtest.T) {
		Passkeys = test.NewMockCollection(mt, "passkeys", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.passkeys", mtest.FirstBatch, bson.D{
			{Key: "id", Value: "invalid_id_not_int64"},
		}))

		passkeys, err := MongoLoadUserPasskeys(ctx, 42)
		assert.Nil(mt, passkeys)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 5. Success
	mt.Run("Success", func(mt *mtest.T) {
		Passkeys = test.NewMockCollection(mt, "passkeys", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.passkeys", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(1)},
			{Key: "user_id", Value: int64(42)},
			{Key: "credential_id", Value: "cred-xyz"},
			{Key: "device_name", Value: "MacBook Pro"},
		}))

		passkeys, err := MongoLoadUserPasskeys(ctx, 42)
		require.NoError(mt, err)
		require.Len(mt, passkeys, 1)
		assert.Equal(mt, int64(1), passkeys[0].ID)
		assert.Equal(mt, "cred-xyz", passkeys[0].CredentialID)
	})
}
