package mongo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoLoadNotificationsPaginated(t *testing.T) {
	defer resetCollections()()

	ctx := context.Background()

	// 1. Notifications is nil
	Notifications = nil
	notifs, err := MongoLoadNotificationsPaginated(ctx, 42, 10, 0)
	assert.Nil(t, notifs)
	assertDatabaseError(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. DB error
	mt.Run("DB error", func(mt *mtest.T) {
		Notifications = newMockCollection(mt, "notifications", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		notifs, err := MongoLoadNotificationsPaginated(ctx, 42, 10, 0)
		assert.Nil(mt, notifs)
		assertDatabaseError(mt, err)
	})

	// 3. Empty result
	mt.Run("Empty result", func(mt *mtest.T) {
		Notifications = newMockCollection(mt, "notifications", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.notifications", mtest.FirstBatch))

		notifs, err := MongoLoadNotificationsPaginated(ctx, 42, 10, 0)
		require.NoError(mt, err)
		assert.NotNil(mt, notifs)
		assert.Empty(mt, notifs)
	})

	// 4. Deserialization error
	mt.Run("Deserialization error", func(mt *mtest.T) {
		Notifications = newMockCollection(mt, "notifications", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.notifications", mtest.FirstBatch, bson.D{
			{Key: "id", Value: "invalid_id"},
		}))

		notifs, err := MongoLoadNotificationsPaginated(ctx, 42, 10, 0)
		assert.Nil(mt, notifs)
		assertDatabaseError(mt, err)
	})

	// 5. Success
	mt.Run("Success", func(mt *mtest.T) {
		Notifications = newMockCollection(mt, "notifications", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.notifications", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(1)},
			{Key: "user_id", Value: int64(42)},
			{Key: "type", Value: "follow"},
			{Key: "is_read", Value: true},
		}))

		notifs, err := MongoLoadNotificationsPaginated(ctx, 42, 10, 0)
		require.NoError(mt, err)
		require.Len(mt, notifs, 1)
		assert.Equal(mt, int64(1), notifs[0].ID)
		assert.Equal(mt, "follow", notifs[0].Type)
	})
}
