package mongo

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMongoDeleteMediaByIDs(t *testing.T) {
	defer test.ResetCollections()

	// 1. Media is nil
	Media = nil
	err := MongoDeleteMediaByIDs([]int64{1, 2, 3})
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("Empty IDs returns nil", func(mt *mtest.T) {
		Media = test.NewMockCollection(mt, "media", nil)
		err := MongoDeleteMediaByIDs([]int64{})
		assert.NoError(mt, err)
	})

	mt.Run("DB error on delete", func(mt *mtest.T) {
		Media = test.NewMockCollection(mt, "media", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "delete failed"}))

		err := MongoDeleteMediaByIDs([]int64{10, 20})
		test.AssertDatabaseError_Mongo(mt, err)
	})

	mt.Run("Delete success", func(mt *mtest.T) {
		Media = test.NewMockCollection(mt, "media", nil)
		mt.AddMockResponses(mtest.CreateSuccessResponse())

		err := MongoDeleteMediaByIDs([]int64{10, 20})
		require.NoError(mt, err)
	})
}
