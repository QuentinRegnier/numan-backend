package mongo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoGetRelationState(t *testing.T) {
	defer resetCollections()()

	ctx := context.Background()

	// 1. Relations is nil
	Relations = nil
	state, err := MongoGetRelationState(ctx, 10, 20)
	assert.Equal(t, 0, state)
	assertDatabaseError(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 2. DB error
	mt.Run("DB error", func(mt *mtest.T) {
		Relations = newMockCollection(mt, "relations", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "failed"}))

		state, err := MongoGetRelationState(ctx, 10, 20)
		assert.Equal(mt, 0, state)
		assertDatabaseError(mt, err)
	})

	// 3. Not found
	mt.Run("Not found", func(mt *mtest.T) {
		Relations = newMockCollection(mt, "relations", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.relations", mtest.FirstBatch))

		state, err := MongoGetRelationState(ctx, 10, 20)
		assert.Equal(mt, 0, state)
		assertNotFoundError(mt, err, "RELATION_NOT_FOUND")
	})

	// 4. Invalid state format
	mt.Run("Invalid state format", func(mt *mtest.T) {
		Relations = newMockCollection(mt, "relations", nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.relations", mtest.FirstBatch, bson.D{
			{Key: "state", Value: "invalid_string_state"},
		}))

		state, err := MongoGetRelationState(ctx, 10, 20)
		assert.Equal(mt, 0, state)
		assertDatabaseError(mt, err)
	})

	// 5. Success with various state types
	tests := []struct {
		name     string
		val      any
		expected int
	}{
		{"float64", float64(1), 1},
		{"int32", int32(2), 2},
		{"int64", int64(3), 3},
		{"int", int(4), 4},
	}

	for _, tc := range tests {
		mt.Run("Success "+tc.name, func(mt *mtest.T) {
			Relations = newMockCollection(mt, "relations", nil)
			mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.relations", mtest.FirstBatch, bson.D{
				{Key: "state", Value: tc.val},
			}))

			state, err := MongoGetRelationState(ctx, 10, 20)
			require.NoError(mt, err)
			assert.Equal(mt, tc.expected, state)
		})
	}
}
