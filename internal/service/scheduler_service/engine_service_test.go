package scheduler_service_test

import (
	"context"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngineCooperativePreemptionAndAntiStarvation(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	now := time.Date(2026, 6, 1, 4, 30, 0, 0, time.UTC)
	cfg := scheduler_service.DefaultRegimeConfig()
	cfg.DownshiftHysteresis = 0
	eng := scheduler_service.NewEngine("dc-eu-1", cfg, func() time.Time { return now })
	eng.SetLocation(time.UTC)

	started := make(chan struct{})
	preempted := make(chan struct{})
	eng.Register(scheduler_service.JobDefinition{
		Name:         "heavy_preemptable",
		Category:     "hygiene",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  time.Hour,
		MaxStaleness: 48 * time.Hour,
		Essential:    true,
		Fn: func(jobCtx context.Context, cp scheduler_models.JobCheckpoint, _ bool) (scheduler_models.JobCheckpoint, bool, error) {
			cp.ItemsTotal = 100
			cp.ItemsDone = 40
			close(started)
			<-jobCtx.Done()
			close(preempted)
			return cp, false, jobCtx.Err()
		},
	})

	eng.EvaluateSample(ctx, scheduler_models.ActivitySample{ActivityScore: 0.02})
	assert.Equal(t, scheduler_models.RegimeDeepIdle, eng.Tracker().Current())

	doneTick := make(chan bool, 1)
	go func() {
		doneTick <- eng.Tick(ctx)
	}()
	<-started

	// Pic de trafic immédiat -> préemption coopérative < 200ms
	t0 := time.Now()
	eng.EvaluateSample(ctx, scheduler_models.ActivitySample{ActivityScore: 0.75})
	select {
	case <-preempted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("la préemption coopérative a dépassé 200ms")
	}
	assert.Less(t, time.Since(t0), 200*time.Millisecond)
	assert.True(t, <-doneTick)

	ov := eng.StatusSnapshot(ctx)
	require.Len(t, ov.Jobs, 1)
	assert.Equal(t, "preempted", ov.Jobs[0].Status)
	assert.Equal(t, int64(40), ov.Jobs[0].Checkpoint.ItemsDone)

	// SLA anti-famine 48h : après 49h sans DEEP_IDLE, le job essentiel bascule en alerte + trickle mode
	now = now.Add(49 * time.Hour)
	trickleRan := false
	eng.Register(scheduler_service.JobDefinition{
		Name:         "heavy_preemptable",
		Category:     "hygiene",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  time.Hour,
		MaxStaleness: 48 * time.Hour,
		Essential:    true,
		Fn: func(_ context.Context, cp scheduler_models.JobCheckpoint, trickle bool) (scheduler_models.JobCheckpoint, bool, error) {
			trickleRan = trickle
			cp.ItemsDone = 100
			cp.ItemsTotal = 100
			return cp, true, nil
		},
	})
	assert.True(t, eng.Tick(ctx))
	assert.True(t, trickleRan)
}
