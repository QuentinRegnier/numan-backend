package scheduler_service_test

import (
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/stretchr/testify/assert"
)

func TestRegimeClassificationAndHysteresis(t *testing.T) {
	now := time.Date(2026, 6, 1, 3, 0, 0, 0, time.UTC)
	cfg := scheduler_service.DefaultRegimeConfig()
	tracker := scheduler_service.NewRegimeTracker(cfg, func() time.Time { return now })

	assert.Equal(t, scheduler_models.RegimeModerate, tracker.Current())

	// Escalade immédiate vers OVERLOAD (0s d'hystérésis)
	reg, changed := tracker.Update(scheduler_models.ActivitySample{
		ActivityScore:   0.20,
		SaturationScore: 0.90,
	})
	assert.True(t, changed)
	assert.Equal(t, scheduler_models.RegimeOverload, reg)

	// Descente vers DEEP_IDLE bloquée tant que DownshiftHysteresis n'est pas écoulée
	reg, changed = tracker.Update(scheduler_models.ActivitySample{ActivityScore: 0.05})
	assert.False(t, changed)
	assert.Equal(t, scheduler_models.RegimeOverload, reg)

	now = now.Add(4 * time.Minute)
	reg, changed = tracker.Update(scheduler_models.ActivitySample{ActivityScore: 0.05})
	assert.True(t, changed)
	assert.Equal(t, scheduler_models.RegimeDeepIdle, reg)

	// Escalade immédiate vers PEAK depuis DEEP_IDLE
	reg, changed = tracker.Update(scheduler_models.ActivitySample{ActivityScore: 0.70})
	assert.True(t, changed)
	assert.Equal(t, scheduler_models.RegimePeak, reg)
}

func TestResolveRegimeConfigFromEnv(t *testing.T) {
	t.Setenv("NUMAN_SCHEDULER_OVERLOAD_THRESHOLD", "0.92")
	t.Setenv("NUMAN_SCHEDULER_HYSTERESIS_SEC", "45")

	cfg := scheduler_service.ResolveRegimeConfigFromEnv()
	assert.InDelta(t, 0.92, cfg.OverloadThreshold, 0.0001)
	assert.Equal(t, 45*time.Second, cfg.DownshiftHysteresis)
}
