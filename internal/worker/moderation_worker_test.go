package worker

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModerationWorker_AgingBatchAndLockExpirations(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	baseTime := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	restoreClock := SetModerationWorkerClock(func() time.Time {
		return baseTime
	})
	defer restoreClock()

	// 1. Vieillissement quadratique M_temps sur ticket standard vs préservation stricte de +Inf
	tStandard := &moderation_models.ModerationTicketRecord{
		ReportID:        1001,
		TargetType:      variables.ReportTargetPost,
		TargetID:        5001,
		State:           variables.ReportStatePending,
		StaticBaseScore: 10.0,
		PriorityScore:   10.0,
		CreatedAt:       baseTime.Add(-12 * time.Hour).UnixMilli(), // 12h d'âge -> M_temps = 1 + (12/12)^2 = 2.0 -> Score = 20.0
	}
	tUrgent := &moderation_models.ModerationTicketRecord{
		ReportID:         1002,
		TargetType:       variables.ReportTargetPost,
		TargetID:         5002,
		State:            variables.ReportStatePending,
		StaticBaseScore:  math.Inf(1),
		PriorityScore:    math.Inf(1),
		IsInfiniteUrgent: true,
		CreatedAt:        baseTime.Add(-24 * time.Hour).UnixMilli(),
	}

	require.NoError(t, cache_service.SaveModerationTicket(ctx, tStandard))
	require.NoError(t, cache_service.SaveModerationTicket(ctx, tUrgent))
	require.NoError(t, cache_service.IndexTicketInModerationQueue(ctx, variables.ModerationQueueActive, tStandard.ReportID, tStandard.PriorityScore))
	require.NoError(t, cache_service.IndexTicketInModerationQueue(ctx, variables.ModerationQueueActive, tUrgent.ReportID, tUrgent.PriorityScore))

	recomputed, err := ProcessModerationAgingBatch(ctx, baseTime)
	require.NoError(t, err)
	assert.Equal(t, 1, recomputed, "seul le ticket standard doit être recalculé (+Inf est préservé sans recalcul)")

	recs, err := cache_service.ListModerationQueueRecords(ctx, variables.ModerationQueueActive, 0, 10)
	require.NoError(t, err)
	require.Len(t, recs, 2)
	assert.Equal(t, int64(1002), recs[0].ReportID, "le ticket +Inf doit rester en tête de file")
	assert.True(t, math.IsInf(recs[0].PriorityScore, 1))

	assert.Equal(t, int64(1001), recs[1].ReportID)
	assert.InDelta(t, 20.0, recs[1].PriorityScore, 1e-4, "M_temps(12h)=2.0 -> 10.0 * 2.0 = 20.0")

	// 2. Expiration automatique d'un verrou InProgress après 48h
	tInProgress := &moderation_models.ModerationTicketRecord{
		ReportID:        1003,
		TargetType:      variables.ReportTargetPost,
		TargetID:        5003,
		State:           variables.ReportStateInProgress,
		ClaimedByModID:  9001,
		PriorityScore:   15.0,
		LockExpiresAtMs: baseTime.Add(-1 * time.Hour).UnixMilli(), // Expiré depuis 1h
		CreatedAt:       baseTime.Add(-49 * time.Hour).UnixMilli(),
	}
	require.NoError(t, cache_service.SaveModerationTicket(ctx, tInProgress))
	// Simuler le verrou dans le ZSET d'expiration
	_, errLock := cache_service.AcquireTicketLock(ctx, 1003, 9001, tInProgress.LockExpiresAtMs, 48*time.Hour)
	require.NoError(t, errLock)

	released, errRelease := ProcessModerationLockExpirations(ctx, baseTime)
	require.NoError(t, errRelease)
	assert.Equal(t, 1, released)

	updatedTicket, errGet := cache_service.GetModerationTicket(ctx, 1003)
	require.NoError(t, errGet)
	assert.Equal(t, variables.ReportStatePending, updatedTicket.State, "le ticket expiré doit être revenu en Pending")
	assert.Equal(t, int64(0), updatedTicket.ClaimedByModID)

	// 3. Expiration d'un minuteur WaitingForInfo après 7 jours
	tWaiting := &moderation_models.ModerationTicketRecord{
		ReportID:       1004,
		TargetType:     variables.ReportTargetPost,
		TargetID:       5004,
		State:          variables.ReportStateWaitingForInfo,
		ClaimedByModID: 9002,
		WaitingUntilMs: baseTime.Add(-2 * time.Hour).UnixMilli(), // 7 jours écoulés
		CreatedAt:      baseTime.Add(-8 * 24 * time.Hour).UnixMilli(),
	}
	require.NoError(t, cache_service.SaveModerationTicket(ctx, tWaiting))
	require.NoError(t, cache_service.ScheduleTicketWaitingInfoExpiration(ctx, 1004, tWaiting.WaitingUntilMs))

	resumed, errResume := ProcessModerationWaitingInfoExpirations(ctx, baseTime)
	require.NoError(t, errResume)
	assert.Equal(t, 1, resumed)

	resumedTicket, errGetResumed := cache_service.GetModerationTicket(ctx, 1004)
	require.NoError(t, errGetResumed)
	assert.Equal(t, variables.ReportStateInProgress, resumedTicket.State, "le ticket WaitingForInfo expiré doit revenir en InProgress pour le même modérateur")
	assert.Equal(t, int64(9002), resumedTicket.ClaimedByModID)
}

func TestModerationWorker_WaitingForInfoSevenDayExpiry(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	startTime := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	currentTime := startTime

	// Injecte l'horloge personnalisée contrôlable
	restoreClock := SetModerationWorkerClock(func() time.Time {
		return currentTime
	})
	defer restoreClock()

	modID := int64(7001)
	ticketID := int64(9901)
	waitingUntil := startTime.Add(variables.ModerationWaitingInfoTTL) // + 7 jours

	ticket := &moderation_models.ModerationTicketRecord{
		ReportID:       ticketID,
		TargetType:     variables.ReportTargetPost,
		TargetID:       5555,
		State:          variables.ReportStateWaitingForInfo,
		ClaimedByModID: modID,
		WaitingUntilMs: waitingUntil.UnixMilli(),
		CreatedAt:      startTime.UnixMilli(),
		UpdatedAt:      startTime.UnixMilli(),
	}
	require.NoError(t, cache_service.SaveModerationTicket(ctx, ticket))
	require.NoError(t, cache_service.ScheduleTicketWaitingInfoExpiration(ctx, ticketID, ticket.WaitingUntilMs))

	// Étape 1 : À J+6 (avant l'échéance des 7 jours), le worker ne doit rien expirer
	currentTime = startTime.Add(6 * 24 * time.Hour)
	expiredCount1, err1 := ProcessModerationWaitingInfoExpirations(ctx, currentTime)
	require.NoError(t, err1)
	assert.Equal(t, 0, expiredCount1, "À J+6, aucun ticket ne doit être expiré")

	tCheck1, errCheck1 := cache_service.GetModerationTicket(ctx, ticketID)
	require.NoError(t, errCheck1)
	assert.Equal(t, variables.ReportStateWaitingForInfo, tCheck1.State, "Le ticket doit rester en WaitingForInfo à J+6")

	// Étape 2 : À J+7 + 1 minute (délai dépassé), le worker débloque le ticket
	currentTime = waitingUntil.Add(1 * time.Minute)
	expiredCount2, err2 := ProcessModerationWaitingInfoExpirations(ctx, currentTime)
	require.NoError(t, err2)
	assert.Equal(t, 1, expiredCount2, "À J+7 + 1min, le ticket doit être expiré et débloqué")

	tCheck2, errCheck2 := cache_service.GetModerationTicket(ctx, ticketID)
	require.NoError(t, errCheck2)
	assert.Equal(t, variables.ReportStateInProgress, tCheck2.State, "Le ticket doit être restitué en InProgress")
	assert.Equal(t, modID, tCheck2.ClaimedByModID, "Le ticket doit être assigné au même modérateur")
	assert.Equal(t, int64(0), tCheck2.WaitingUntilMs)
	assert.True(t, tCheck2.LockExpiresAtMs > currentTime.UnixMilli(), "Un nouveau verrou exclusif 48h doit être posé")
}
