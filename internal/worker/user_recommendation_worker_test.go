package worker

import (
	"context"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func seedWorkerRecoUser(t *testing.T, ctx context.Context, id int64, username string, active bool, banned bool) {
	t.Helper()
	nowMs := domain.NowMillis()
	updatedAt := nowMs
	if !active {
		updatedAt = domain.TimeToMillis(domain.NowTime().Add(-90 * 24 * time.Hour))
	}

	u := auth_models.UserPayload{
		ID:        id,
		Username:  username,
		FirstName: username,
		LastName:  "WorkerTest",
		Location:  "Paris",
		Banned:    banned,
		CreatedAt: updatedAt,
		UpdatedAt: updatedAt,
	}
	require.NoError(t, redis.Users.SetObject(ctx, id, u))

	s := user_settings_models.UserSettingsPayload{
		ID:     id,
		UserID: id,
		Privacy: user_settings_models.PrivacySettings{
			ShowLocation: true,
		},
		DisplayAndContent: user_settings_models.DisplayAndContentSettings{
			Language: 0,
		},
	}
	require.NoError(t, object_cache_service.SetUserSettings(ctx, s))
	require.NoError(t, cache_service.AddUserToSpeedCache(ctx, u, s))
}

func TestHandleUserRecommendationEvents_ProcessBatch(t *testing.T) {
	_ = test.SetupTestRedis(t)
	defer test.ResetCollections()()
	mongo.UserRecoSkips = nil
	ctx := context.Background()

	seedWorkerRecoUser(t, ctx, 701, "user_701", true, false)
	seedWorkerRecoUser(t, ctx, 702, "user_702", true, false)

	events := []redis.AsyncEvent{
		{
			ID:      701,
			Type:    redis.EntityUserRecommendation,
			Action:  redis.ActionGenerateUserRecommendations,
			Targets: redis.TargetWorker,
			Payload: user_recommendation_models.GenerateUserRecommendationsJobPayload{
				UserID:      701,
				Force:       true,
				TriggeredBy: "test",
				RequestedAt: domain.NowMillis(),
			},
		},
		// Duplicate in same batch should be deduplicated
		{
			ID:      701,
			Type:    redis.EntityUserRecommendation,
			Action:  redis.ActionGenerateUserRecommendations,
			Targets: redis.TargetWorker,
			Payload: user_recommendation_models.GenerateUserRecommendationsJobPayload{
				UserID:      701,
				Force:       false,
				TriggeredBy: "test_dup",
				RequestedAt: domain.NowMillis(),
			},
		},
	}

	processBatch(ctx, events)

	ids, hit, err := cache_service.GetUserRecommendationIDs(ctx, 701, variables.UserRecoSortGlobal, 10)
	require.NoError(t, err)
	assert.True(t, hit)
	assert.Equal(t, []int64{702}, ids)
}

func TestRunUserRecommendationRefreshCronCycle_BatchingLockAndGracefulShutdown(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	origBatchSize := variables.UserRecoCronBatchSize
	variables.UserRecoCronBatchSize = 2
	defer func() {
		variables.UserRecoCronBatchSize = origBatchSize
	}()

	// Seed 4 users:
	// - 801: active & eligible
	// - 802: active & eligible, but already has compute lock
	// - 803: inactive (90 days ago)
	// - 804: banned
	seedWorkerRecoUser(t, ctx, 801, "active_801", true, false)
	seedWorkerRecoUser(t, ctx, 802, "locked_802", true, false)
	seedWorkerRecoUser(t, ctx, 803, "inactive_803", false, false)
	seedWorkerRecoUser(t, ctx, 804, "banned_804", true, true)

	locked, err := cache_service.AcquireUserRecoComputeLock(ctx, 802)
	require.NoError(t, err)
	require.True(t, locked)

	stats := RunUserRecommendationRefreshCronCycle(ctx)
	assert.Equal(t, 4, stats.UsersScanned)
	assert.Equal(t, 1, stats.JobsEnqueued)
	assert.Equal(t, 1, stats.SkippedLocked)
	assert.Equal(t, 2, stats.SkippedInactive)
	assert.Equal(t, 0, stats.ErrorsCount)

	// Running a second cycle immediately should skip 801 as well because its compute lock is now set
	stats2 := RunUserRecommendationRefreshCronCycle(ctx)
	assert.Equal(t, 0, stats2.JobsEnqueued)
	assert.Equal(t, 2, stats2.SkippedLocked)

	// Verify graceful shutdown via cancelled context
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	startUserRecommendationCron(cancelCtx)
	statsCancelled := RunUserRecommendationRefreshCronCycle(cancelCtx)
	assert.Equal(t, 0, statsCancelled.JobsEnqueued)
}

func TestFlushMongo_EntityUserRecoSkip(t *testing.T) {
	defer test.ResetCollections()()
	ctx := context.Background()

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("persists_skip_events_via_bulk_upsert", func(mt *mtest.T) {
		mongo.UserRecoSkips = test.NewMockCollection(mt, variables.MongoCollUserRecoSkips, nil)
		mt.AddMockResponses(bson.D{
			{Key: "ok", Value: 1},
			{Key: "n", Value: 1},
		})

		flushMongo(ctx, []redis.AsyncEvent{
			{
				ID:      10,
				Type:    redis.EntityUserRecoSkip,
				Action:  redis.ActionCreate,
				Targets: redis.TargetMongo,
				Payload: user_recommendation_models.UserRecoSkipPayload{
					UserID:        10,
					SkippedUserID: 20,
					CreatedAt:     time.Now().UTC(),
				},
			},
		})
	})
}
