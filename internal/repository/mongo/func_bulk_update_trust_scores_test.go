package mongo_test

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestBulkUpdateUserTrustScores(t *testing.T) {
	defer test.ResetCollections()
	ctx := context.Background()

	// 1. Uninitialized collection
	mongo.Users = nil
	errUninit := mongo.BulkUpdateUserTrustScores(ctx, []mongo.UserTrustScoreUpdate{{UserID: 1, TrustScore: 0.50}})
	assert.Error(t, errUninit)
	test.AssertDatabaseError_Mongo(t, errUninit)

	// 2. Empty batch returns immediately without error
	require.NoError(t, mongo.BulkUpdateUserTrustScores(ctx, nil))

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("successful_bulk_write", func(mt *mtest.T) {
		mongo.Users = test.NewMockCollection(mt, "auth.users", nil)
		mt.AddMockResponses(bson.D{
			{Key: "ok", Value: 1},
			{Key: "n", Value: 2},
			{Key: "nModified", Value: 2},
		})

		updates := []mongo.UserTrustScoreUpdate{
			{UserID: 101, TrustScore: 0.45, QuarantinePostsApproved: 1},
			{UserID: 102, TrustScore: 0.88, LastInfractionAt: 0},
		}
		err := mongo.BulkUpdateUserTrustScores(ctx, updates)
		require.NoError(mt, err)
	})

	mt.Run("bulk_write_error", func(mt *mtest.T) {
		mongo.Users = test.NewMockCollection(mt, "auth.users", nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "bulk write failure"}))

		updates := []mongo.UserTrustScoreUpdate{
			{UserID: 101, TrustScore: 0.45},
		}
		err := mongo.BulkUpdateUserTrustScores(ctx, updates)
		assert.Error(mt, err)
		test.AssertDatabaseError_Mongo(mt, err)
	})
}
