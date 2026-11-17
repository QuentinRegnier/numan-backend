package mongo_test

import (
	"context"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestBulkUpsertUserRecoSkips(t *testing.T) {
	defer test.ResetCollections()()
	ctx := context.Background()

	// 1. Empty slice returns nil immediately
	require.NoError(t, mongo.BulkUpsertUserRecoSkips(ctx, nil))

	// 2. Uninitialized collection returns AppError
	mongo.UserRecoSkips = nil
	err := mongo.BulkUpsertUserRecoSkips(ctx, []user_recommendation_models.UserRecoSkipPayload{
		{UserID: 10, SkippedUserID: 20, CreatedAt: time.Now().UTC()},
	})
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 3. Invalid skips only (self-skip or non-positive IDs) -> no DB call, returns nil
	mt.Run("all_invalid_skips_ignored", func(mt *mtest.T) {
		mongo.UserRecoSkips = test.NewMockCollection(mt, variables.MongoCollUserRecoSkips, nil)
		err := mongo.BulkUpsertUserRecoSkips(ctx, []user_recommendation_models.UserRecoSkipPayload{
			{UserID: 10, SkippedUserID: 10},
			{UserID: 0, SkippedUserID: 20},
			{UserID: 10, SkippedUserID: -5},
		})
		require.NoError(mt, err)
	})

	// 4. Successful idempotent bulk upsert (including duplicate skip events)
	mt.Run("success_bulk_upsert_and_duplicate_handling", func(mt *mtest.T) {
		mongo.UserRecoSkips = test.NewMockCollection(mt, variables.MongoCollUserRecoSkips, nil)
		mt.AddMockResponses(bson.D{
			{Key: "ok", Value: 1},
			{Key: "n", Value: 2},
			{Key: "nModified", Value: 1},
		})

		skips := []user_recommendation_models.UserRecoSkipPayload{
			{UserID: 10, SkippedUserID: 20, CreatedAt: time.Now().UTC()},
			{UserID: 10, SkippedUserID: 20}, // Duplicate with zero CreatedAt
			{UserID: 10, SkippedUserID: 30, CreatedAt: time.Now().UTC()},
		}
		err := mongo.BulkUpsertUserRecoSkips(ctx, skips)
		require.NoError(mt, err)
	})

	// 5. DB error on BulkWrite
	mt.Run("bulk_write_error", func(mt *mtest.T) {
		mongo.UserRecoSkips = test.NewMockCollection(mt, variables.MongoCollUserRecoSkips, nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{
			Code:    96,
			Message: "bulk write failed",
		}))

		skips := []user_recommendation_models.UserRecoSkipPayload{
			{UserID: 10, SkippedUserID: 20, CreatedAt: time.Now().UTC()},
		}
		err := mongo.BulkUpsertUserRecoSkips(ctx, skips)
		test.AssertDatabaseError_Mongo(mt, err)
	})
}

func TestMongoLoadActiveUserRecoSkips(t *testing.T) {
	defer test.ResetCollections()()
	ctx := context.Background()

	// 1. Non-positive userID returns empty non-nil slice
	ids, err := mongo.MongoLoadActiveUserRecoSkips(ctx, 0)
	require.NoError(t, err)
	assert.Empty(t, ids)

	// 2. Uninitialized collection returns AppError
	mongo.UserRecoSkips = nil
	ids, err = mongo.MongoLoadActiveUserRecoSkips(ctx, 42)
	assert.Nil(t, ids)
	test.AssertDatabaseError_Mongo(t, err)

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	// 3. DB error on Find
	mt.Run("db_error", func(mt *mtest.T) {
		mongo.UserRecoSkips = test.NewMockCollection(mt, variables.MongoCollUserRecoSkips, nil)
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 96, Message: "find failed"}))

		ids, err := mongo.MongoLoadActiveUserRecoSkips(ctx, 42)
		assert.Nil(mt, ids)
		test.AssertDatabaseError_Mongo(mt, err)
	})

	// 4. Empty result
	mt.Run("empty_result", func(mt *mtest.T) {
		mongo.UserRecoSkips = test.NewMockCollection(mt, variables.MongoCollUserRecoSkips, nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.user_reco_skips", mtest.FirstBatch))

		ids, err := mongo.MongoLoadActiveUserRecoSkips(ctx, 42)
		require.NoError(mt, err)
		assert.NotNil(mt, ids)
		assert.Empty(mt, ids)
	})

	// 5. Success with deduplication and self-skip filtering
	mt.Run("success_deduplicated", func(mt *mtest.T) {
		mongo.UserRecoSkips = test.NewMockCollection(mt, variables.MongoCollUserRecoSkips, nil)
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.user_reco_skips", mtest.FirstBatch,
			bson.D{{Key: "skipped_user_id", Value: int64(101)}},
			bson.D{{Key: "skipped_user_id", Value: int64(102)}},
			bson.D{{Key: "skipped_user_id", Value: int64(101)}}, // duplicate
			bson.D{{Key: "skipped_user_id", Value: int64(42)}},  // self-skip ignored
			bson.D{{Key: "skipped_user_id", Value: int64(0)}},   // invalid ignored
		))

		ids, err := mongo.MongoLoadActiveUserRecoSkips(ctx, 42)
		require.NoError(mt, err)
		assert.Equal(mt, []int64{101, 102}, ids)
	})
}
