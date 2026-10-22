package mongo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoGetMember(t *testing.T) {
	defer resetCollections()()

	ctx := context.Background()

	// 1. Uninitialized
	Members = nil
	Messages = nil
	m, err := MongoGetMember(ctx, 10, 20)
	assert.Empty(t, m.ID)
	assertDatabaseError(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. DB query error
	mt.Run("DB query error", func(mt *mtest.T) {
		Members = newMockCollection(mt, "members", nil)
		Messages = newMockCollection(mt, "messages", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "find failed"}))

		m, err := MongoGetMember(ctx, 10, 20)
		assert.Empty(mt, m.ID)
		assertDatabaseError(mt, err)
	})

	// 3. Not found
	mt.Run("Not found (empty results)", func(mt *mtest.T) {
		Members = newMockCollection(mt, "members", nil)
		Messages = newMockCollection(mt, "messages", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.members", mtest.FirstBatch))

		m, err := MongoGetMember(ctx, 10, 20)
		assert.Empty(mt, m.ID)
		assertNotFoundError(mt, err, "MEMBER_NOT_FOUND")
	})

	// 4. Deserialization error
	mt.Run("Deserialization error", func(mt *mtest.T) {
		Members = newMockCollection(mt, "members", nil)
		Messages = newMockCollection(mt, "messages", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.members", mtest.FirstBatch, bson.D{
			{Key: "id", Value: "invalid_id_type"},
		}))

		m, err := MongoGetMember(ctx, 10, 20)
		assert.Empty(mt, m.ID)
		assertDatabaseError(mt, err)
	})

	// 5. Success
	mt.Run("Success with recount", func(mt *mtest.T) {
		Members = newMockCollection(mt, "members", nil)
		Messages = newMockCollection(mt, "messages", nil)

		nowMillis := time.Now().UnixMilli()
		mt.AddMockResponses(
			// Member find
			mtest.CreateCursorResponse(0, "test.members", mtest.FirstBatch, bson.D{
				{Key: "id", Value: int64(101)},
				{Key: "conversation_id", Value: int64(10)},
				{Key: "user_id", Value: int64(20)},
				{Key: "unread_count", Value: int32(2)},
				{Key: "updated_at", Value: nowMillis},
			}),
		)

		m, err := MongoGetMember(ctx, 10, 20)
		require.NoError(mt, err)
		assert.Equal(mt, int64(101), m.ID)
		assert.Equal(mt, 2, m.UnreadCount)
	})
}
