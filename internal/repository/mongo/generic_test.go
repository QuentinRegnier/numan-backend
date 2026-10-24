package mongo

import (
	"context"
	"reflect"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestCollection_NilReceiver(t *testing.T) {
	var nilColl *Collection

	// Set
	err := nilColl.Set(map[string]any{"id": int64(1)})
	test.AssertDatabaseError_Mongo(t, err)

	// Get
	docs, err := nilColl.Get(map[string]any{"id": int64(1)}, nil)
	assert.Nil(t, docs)
	test.AssertDatabaseError_Mongo(t, err)

	// Delete
	err = nilColl.Delete(map[string]any{"id": int64(1)})
	test.AssertDatabaseError_Mongo(t, err)

	// update
	err = nilColl.update(map[string]any{"id": int64(1)}, map[string]any{"id": int64(1)})
	test.AssertDatabaseError_Mongo(t, err)

	// GetPaginated
	docs, err = nilColl.GetPaginated(map[string]any{"id": int64(1)}, nil, 0, 10)
	assert.Nil(t, docs)
	test.AssertDatabaseError_Mongo(t, err)

	// validate
	err = nilColl.validate(context.Background(), map[string]any{"id": int64(1)}, false)
	test.AssertDatabaseError_Mongo(t, err)

	// Collection with nil DB
	collNilDB := &Collection{Name: "test", DB: nil}
	test.AssertDatabaseError_Mongo(t, collNilDB.Set(map[string]any{"id": int64(1)}))
	docs, err = collNilDB.Get(map[string]any{"id": int64(1)}, nil)
	assert.Nil(t, docs)
	test.AssertDatabaseError_Mongo(t, err)
	test.AssertDatabaseError_Mongo(t, collNilDB.Delete(map[string]any{"id": int64(1)}))
	test.AssertDatabaseError_Mongo(t, collNilDB.update(map[string]any{"id": int64(1)}, map[string]any{"id": int64(1)}))
	docs, err = collNilDB.GetPaginated(map[string]any{"id": int64(1)}, nil, 0, 10)
	assert.Nil(t, docs)
	test.AssertDatabaseError_Mongo(t, err)
}

func TestCollection_Validate(t *testing.T) {
	schema := map[string]reflect.Kind{
		"id":   reflect.Int64,
		"name": reflect.String,
	}
	coll := &Collection{Name: "test", Schema: schema}

	// Missing required field (partial = false)
	err := coll.validate(context.Background(), map[string]any{"id": int64(1)}, false)
	test.AssertDatabaseError_Mongo(t, err)

	// Inconsistent type
	err = coll.validate(context.Background(), map[string]any{"id": "not an int", "name": "foo"}, false)
	test.AssertDatabaseError_Mongo(t, err)

	// Partial valid
	err = coll.validate(context.Background(), map[string]any{"id": int64(2)}, true)
	assert.NoError(t, err)

	// Complete valid
	err = coll.validate(context.Background(), map[string]any{"id": int64(2), "name": "hello"}, false)
	assert.NoError(t, err)
}

func TestCollection_Operations(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	schema := map[string]reflect.Kind{
		"id":   reflect.Int64,
		"name": reflect.String,
	}

	mt.Run("Set success and error", func(mt *mtest.T) {
		coll := test.NewMockCollection(mt, "items", schema)

		// DB error on UpdateOne
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 11000, Message: "duplicate"}))
		err := coll.Set(map[string]any{"id": int64(1), "name": "foo"})
		test.AssertDatabaseError_Mongo(t, err)

		// DB success
		mt.AddMockResponses(mtest.CreateSuccessResponse())
		err = coll.Set(map[string]any{"id": int64(1), "name": "foo"})
		assert.NoError(t, err)
	})

	mt.Run("Get success, empty and error", func(mt *mtest.T) {
		coll := test.NewMockCollection(mt, "items", schema)

		// DB error on Find
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "operation failed"}))
		docs, err := coll.Get(map[string]any{"id": int64(1)}, nil)
		assert.Nil(t, docs)
		test.AssertDatabaseError_Mongo(t, err)

		// DB empty result returns non-nil empty slice
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.items", mtest.FirstBatch))
		docs, err = coll.Get(map[string]any{"id": int64(999)}, nil)
		assert.NoError(t, err)
		assert.NotNil(t, docs)
		assert.Empty(t, docs)

		// DB success with docs
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.items", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(1)},
			{Key: "name", Value: "hello"},
		}))
		docs, err = coll.Get(map[string]any{"id": int64(1)}, nil)
		assert.NoError(t, err)
		require.Len(t, docs, 1)
		assert.Equal(t, int64(1), docs[0]["id"])
	})

	mt.Run("Delete success and error", func(mt *mtest.T) {
		coll := test.NewMockCollection(mt, "items", schema)

		// DB error
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "delete failed"}))
		err := coll.Delete(map[string]any{"id": int64(1)})
		test.AssertDatabaseError_Mongo(t, err)

		// DB success
		mt.AddMockResponses(mtest.CreateSuccessResponse())
		err = coll.Delete(map[string]any{"id": int64(1)})
		assert.NoError(t, err)
	})

	mt.Run("update success, validation error and error", func(mt *mtest.T) {
		coll := test.NewMockCollection(mt, "items", schema)

		// Validation error
		err := coll.update(map[string]any{"id": int64(1)}, map[string]any{"name": 12345})
		test.AssertDatabaseError_Mongo(t, err)

		// DB error
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "update failed"}))
		err = coll.update(map[string]any{"id": int64(1)}, map[string]any{"name": "new"})
		test.AssertDatabaseError_Mongo(t, err)

		// DB success
		mt.AddMockResponses(mtest.CreateSuccessResponse())
		err = coll.update(map[string]any{"id": int64(1)}, map[string]any{"name": "new"})
		assert.NoError(t, err)
	})

	mt.Run("GetPaginated success, empty and error", func(mt *mtest.T) {
		coll := test.NewMockCollection(mt, "items", schema)

		// DB error
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "paginated failed"}))
		docs, err := coll.GetPaginated(map[string]any{"id": int64(1)}, nil, 0, 10)
		assert.Nil(t, docs)
		test.AssertDatabaseError_Mongo(t, err)

		// Empty result
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.items", mtest.FirstBatch))
		docs, err = coll.GetPaginated(map[string]any{"id": int64(999)}, nil, 0, 10)
		assert.NoError(t, err)
		assert.NotNil(t, docs)
		assert.Empty(t, docs)

		// Success
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.items", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(1)},
		}))
		docs, err = coll.GetPaginated(map[string]any{"id": int64(1)}, map[string]any{"id": 1}, 0, 10)
		assert.NoError(t, err)
		require.Len(t, docs, 1)
	})
}
