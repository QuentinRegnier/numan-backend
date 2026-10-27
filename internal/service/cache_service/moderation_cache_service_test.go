package cache_service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModerationCacheService_TicketQueuesLocksAndExpirations(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	// 1. Save & Get Ticket + +Inf ZSET Fast-Lane Ordering
	t1 := &moderation_models.ModerationTicketRecord{
		ReportID:        101,
		TargetType:      variables.ReportTargetPost,
		TargetID:        501,
		TargetOwnerID:   1001,
		Category:        variables.ReportCatSpam,
		ReportOrigin:    variables.ReportOriginUser,
		State:           variables.ReportStatePending,
		StaticBaseScore: 12.5,
		PriorityScore:   12.5,
		CreatedAt:       1000,
	}
	t2Urgent := &moderation_models.ModerationTicketRecord{
		ReportID:         102,
		TargetType:       variables.ReportTargetMessage,
		TargetID:         502,
		TargetOwnerID:    1002,
		Category:         variables.ReportCatSelfHarm,
		ReportOrigin:     variables.ReportOriginUser,
		State:            variables.ReportStatePending,
		StaticBaseScore:  math.Inf(1),
		PriorityScore:    math.Inf(1),
		IsInfiniteUrgent: true,
		CreatedAt:        1005,
	}

	require.NoError(t, SaveModerationTicket(ctx, t1))
	require.NoError(t, SaveModerationTicket(ctx, t2Urgent))
	require.NoError(t, IndexTicketInModerationQueue(ctx, variables.ModerationQueueActive, t1.ReportID, t1.PriorityScore))
	require.NoError(t, IndexTicketInModerationQueue(ctx, variables.ModerationQueueActive, t2Urgent.ReportID, t2Urgent.PriorityScore))

	recs, err := ListModerationQueueRecords(ctx, variables.ModerationQueueActive, 0, 10)
	require.NoError(t, err)
	require.Len(t, recs, 2)
	// Le ticket +Inf (102) doit impérativement être en tête de file devant le ticket fini (101)
	assert.Equal(t, int64(102), recs[0].ReportID)
	assert.True(t, math.IsInf(recs[0].PriorityScore, 1))
	assert.Equal(t, int64(101), recs[1].ReportID)

	// 2. Exclusive Lock (SetNX) + Concurrency Rejection + Expiration Index
	nowMs := int64(2000)
	expMs := nowMs + int64((48 * time.Hour).Milliseconds())
	acquired1, err := AcquireTicketLock(ctx, 101, 9001, expMs, 48*time.Hour)
	require.NoError(t, err)
	assert.True(t, acquired1)

	// Deuxième modérateur tentant de réclamer le même ticket : doit échouer
	acquired2, err := AcquireTicketLock(ctx, 101, 9002, expMs, 48*time.Hour)
	require.NoError(t, err)
	assert.False(t, acquired2)

	// Vérifie la détection d'expiration lorsque l'horloge dépasse expMs
	expiredBefore, err := GetExpiredTicketIDs(ctx, variables.ModerationExpirationLocks, expMs-1, 10)
	require.NoError(t, err)
	assert.Empty(t, expiredBefore)

	expiredAfter, err := GetExpiredTicketIDs(ctx, variables.ModerationExpirationLocks, expMs+1, 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{101}, expiredAfter)

	require.NoError(t, ReleaseTicketLock(ctx, 101))
	acquiredAfterRelease, err := AcquireTicketLock(ctx, 101, 9002, expMs, 48*time.Hour)
	require.NoError(t, err)
	assert.True(t, acquiredAfterRelease)

	// 3. WaitingForInfo 7-day Expiration Timer
	waitExpMs := nowMs + int64((7 * 24 * time.Hour).Milliseconds())
	require.NoError(t, ScheduleTicketWaitingInfoExpiration(ctx, 101, waitExpMs))
	expiredWait, err := GetExpiredTicketIDs(ctx, variables.ModerationExpirationWaitingInfo, waitExpMs+10, 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{101}, expiredWait)
	require.NoError(t, ClearTicketWaitingInfoExpiration(ctx, 101))

	// 4. ModeratorStats & Cleanup
	stats, err := GetOrInitModeratorStats(ctx, 9001, variables.UserGradeModerator)
	require.NoError(t, err)
	assert.InDelta(t, variables.ModTrustInitial, stats.ModeratorTrustScore, 1e-6)
	stats.BlameCount = 2
	require.NoError(t, SaveModeratorStats(ctx, stats))

	loadedStats, err := GetOrInitModeratorStats(ctx, 9001, variables.UserGradeModerator)
	require.NoError(t, err)
	assert.Equal(t, 2, loadedStats.BlameCount)

	require.NoError(t, DeleteModerationTicket(ctx, 101))
	_, errNotFound := GetModerationTicket(ctx, 101)
	require.Error(t, errNotFound)
	var appErr *numan_error.AppError
	require.ErrorAs(t, errNotFound, &appErr)
	assert.Equal(t, numan_error.CodeNotFound, appErr.Code)
}
