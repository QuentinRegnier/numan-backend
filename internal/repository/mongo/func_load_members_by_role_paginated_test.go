package mongo

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoLoadMembersByRolePaginated(t *testing.T) {
	defer test.ResetCollections()

	// 1. Members is nil
	Members = nil
	members, err := MongoLoadMembersByRolePaginated(10, 1, 10, 0)
	assert.Nil(t, members)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. DB error
	mt.Run("DB error", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		members, err := MongoLoadMembersByRolePaginated(10, 1, 10, 0)
		assert.Nil(mt, members)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 3. Empty result
	mt.Run("Empty result", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.members", mtest.FirstBatch))

		members, err := MongoLoadMembersByRolePaginated(10, 1, 10, 0)
		require.NoError(mt, err)
		assert.NotNil(mt, members)
		assert.Empty(mt, members)
	})

	// 4. Deserialization error
	mt.Run("Deserialization error", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.members", mtest.FirstBatch, bson.D{
			{Key: "id", Value: "invalid_id"},
		}))

		members, err := MongoLoadMembersByRolePaginated(10, 1, 10, 0)
		assert.Nil(mt, members)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 5. Success
	mt.Run("Success", func(mt *mtest.T) {
		Members = test.NewMockCollection(mt, "members", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.members", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(101)},
			{Key: "conversation_id", Value: int64(10)},
			{Key: "user_id", Value: int64(50)},
			{Key: "role", Value: int32(1)},
		}))

		members, err := MongoLoadMembersByRolePaginated(10, 1, 10, 0)
		require.NoError(mt, err)
		require.Len(mt, members, 1)
		assert.Equal(mt, int64(101), members[0].ID)
		assert.Equal(mt, 1, members[0].Role)
	})
}
