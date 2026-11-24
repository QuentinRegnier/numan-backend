package cache_service_test

import (
	"context"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchedulerCacheService(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	t.Run("lock, pause, overload, status and remote trigger", func(t *testing.T) {
		tok, ok, err := cache_service.AcquireSchedulerLocalLock(ctx, "job_a", 10*time.Second)
		require.NoError(t, err)
		require.True(t, ok)
		_, ok2, err2 := cache_service.AcquireSchedulerLocalLock(ctx, "job_a", 10*time.Second)
		require.NoError(t, err2)
		assert.False(t, ok2)
		require.NoError(t, cache_service.ReleaseSchedulerLocalLock(ctx, "job_a", tok))

		require.NoError(t, cache_service.SetSchedulerOverloadFlag(ctx, true, time.Minute))
		assert.True(t, cache_service.IsSchedulerOverloadFlagSet(ctx))
		require.NoError(t, cache_service.SetSchedulerOverloadFlag(ctx, false, time.Minute))
		assert.False(t, cache_service.IsSchedulerOverloadFlagSet(ctx))

		require.NoError(t, cache_service.SetSchedulerGlobalPause(ctx, scheduler_models.PausePayload{
			PausedAtMs: time.Now().UnixMilli(),
			Reason:     "upgrade",
		}, time.Minute))
		paused, reason, found := cache_service.GetSchedulerGlobalPause(ctx)
		assert.True(t, found)
		assert.True(t, paused)
		assert.Equal(t, "upgrade", reason)
		require.NoError(t, cache_service.ClearSchedulerGlobalPause(ctx))
		paused, _, found = cache_service.GetSchedulerGlobalPause(ctx)
		assert.True(t, found)
		assert.False(t, paused)

		st := scheduler_models.JobStatus{
			Name:      "job_a",
			Category:  "hygiene",
			MinRegime: scheduler_models.RegimeDeepIdle,
			LockScope: scheduler_models.LockScopeLocalDC,
			Status:    "completed",
		}
		require.NoError(t, cache_service.SaveSchedulerJobStatus(ctx, st, time.Hour))
		got, exists := cache_service.GetSchedulerJobStatus(ctx, "job_a")
		require.True(t, exists)
		assert.Equal(t, "completed", got.Status)
		all, err := cache_service.ListAllSchedulerJobStatuses(ctx)
		require.NoError(t, err)
		require.Len(t, all, 1)

		require.NoError(t, cache_service.EnqueueSchedulerRemoteTrigger(ctx, scheduler_models.RemoteTriggerPayload{
			JobName:       "job_a",
			Force:         true,
			RequestedAtMs: time.Now().UnixMilli(),
		}, time.Minute))
		trig, popped := cache_service.PopSchedulerRemoteTrigger(ctx, "job_a")
		require.True(t, popped)
		assert.True(t, trig.Force)
		_, popped2 := cache_service.PopSchedulerRemoteTrigger(ctx, "job_a")
		assert.False(t, popped2)
	})

	t.Run("surgical most cache purify preserves valid viral scores", func(t *testing.T) {
		require.NoError(t, repoRedis.Posts.SetObject(ctx, 1001, post_models.PostPayload{ID: 1001, Visibility: 1}))
		require.NoError(t, repoRedis.Posts.SetObject(ctx, 1002, post_models.PostPayload{ID: 1002, Visibility: -1}))
		subKey := variables.StrictRankSubKey(variables.StrictTypeRecent, variables.DefaultContentLanguage)
		fullKey := repoRedis.RankedPosts.Key(subKey)
		require.NoError(t, repoRedis.ZAddOnCore(ctx, fullKey, 999.5, "1001"))
		require.NoError(t, repoRedis.ZAddOnCore(ctx, fullKey, 500.0, "1002"))

		done, total, err := cache_service.SurgicallyPurifyMostCache(ctx, nil)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, done, int64(1))
		assert.GreaterOrEqual(t, total, int64(1))

		ids, err := cache_service.GetTopRecentRankedPostIDs(ctx, 10)
		require.NoError(t, err)
		assert.Equal(t, []int64{1001}, ids)
	})
}
