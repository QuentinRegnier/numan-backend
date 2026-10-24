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

func TestMongoGetPasskeyByCredentialID(t *testing.T) {
	defer test.ResetCollections()

	ctx := context.Background()

	// 1. Passkeys is nil
	Passkeys = nil
	p, err := MongoGetPasskeyByCredentialID(ctx, "cred-123")
	assert.Empty(t, p.ID)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. Empty credential ID returns Not Found
	mt.Run("Empty credential ID", func(mt *mtest.T) {
		Passkeys = test.NewMockCollection(mt, "passkeys", nil)
		p, err := MongoGetPasskeyByCredentialID(ctx, "")
		assert.Empty(mt, p.ID)
		test.AssertNotFoundError_Mongo(mt, err, "")
	})

	// 3. DB error
	mt.Run("DB error", func(mt *mtest.T) {
		Passkeys = test.NewMockCollection(mt, "passkeys", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		p, err := MongoGetPasskeyByCredentialID(ctx, "cred-123")
		assert.Empty(mt, p.ID)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 4. Not found
	mt.Run("Not found", func(mt *mtest.T) {
		Passkeys = test.NewMockCollection(mt, "passkeys", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.passkeys", mtest.FirstBatch))

		p, err := MongoGetPasskeyByCredentialID(ctx, "cred-not-found")
		assert.Empty(mt, p.ID)
		test.AssertNotFoundError_Mongo(mt, err, "")
	})

	// 5. Deserialization error
	mt.Run("Deserialization error", func(mt *mtest.T) {
		Passkeys = test.NewMockCollection(mt, "passkeys", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.passkeys", mtest.FirstBatch, bson.D{
			{Key: "id", Value: "invalid_id_type"},
		}))

		p, err := MongoGetPasskeyByCredentialID(ctx, "cred-123")
		assert.Empty(mt, p.ID)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 6. Success
	mt.Run("Success", func(mt *mtest.T) {
		Passkeys = test.NewMockCollection(mt, "passkeys", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.passkeys", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(1001)},
			{Key: "credential_id", Value: "cred-abc"},
			{Key: "device_name", Value: "My iPhone"},
		}))

		p, err := MongoGetPasskeyByCredentialID(ctx, "cred-abc")
		require.NoError(mt, err)
		assert.Equal(mt, int64(1001), p.ID)
		assert.Equal(mt, "cred-abc", p.CredentialID)
		assert.Equal(mt, "My iPhone", p.DeviceName)
	})
}
