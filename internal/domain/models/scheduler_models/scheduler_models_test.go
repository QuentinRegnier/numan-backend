package scheduler_models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchedulerModels_RegimeAndValidation(t *testing.T) {
	t.Run("Regime priority, budget, and Allows", func(t *testing.T) {
		assert.True(t, RegimeDeepIdle.Allows(RegimeDeepIdle))
		assert.True(t, RegimeDeepIdle.Allows(RegimeIdle))
		assert.False(t, RegimeIdle.Allows(RegimeDeepIdle))
		assert.False(t, RegimePeak.Allows(RegimeModerate))
		assert.False(t, RegimeOverload.Allows(RegimeModerate))
		assert.Equal(t, 95, RegimeDeepIdle.CPUBudgetPercent())
		assert.Equal(t, 75, RegimeIdle.CPUBudgetPercent())
		assert.Equal(t, 30, RegimeModerate.CPUBudgetPercent())
		assert.Equal(t, 0, RegimePeak.CPUBudgetPercent())
		assert.Equal(t, 0, RegimeOverload.CPUBudgetPercent())
		assert.True(t, RegimeOverload.IsValid())
		assert.False(t, Regime("UNKNOWN").IsValid())
	})

	t.Run("RegimeConfig validation", func(t *testing.T) {
		var nilCfg *RegimeConfig
		require.Error(t, nilCfg.Validate())

		badOrder := &RegimeConfig{DeepIdleThreshold: 0.4, IdleThreshold: 0.2, ModerateThreshold: 0.6, OverloadThreshold: 0.85}
		require.Error(t, badOrder.Validate())

		outOfRange := &RegimeConfig{DeepIdleThreshold: -0.1, IdleThreshold: 0.25, ModerateThreshold: 0.6, OverloadThreshold: 0.85}
		require.Error(t, outOfRange.Validate())

		valid := &RegimeConfig{
			DeepIdleThreshold:   0.12,
			IdleThreshold:       0.25,
			ModerateThreshold:   0.60,
			OverloadThreshold:   0.85,
			DownshiftHysteresis: 5 * time.Minute,
		}
		require.NoError(t, valid.Validate())
	})

	t.Run("ActivitySample, JobCheckpoint, JobStatus, SchedulerOverview validation", func(t *testing.T) {
		require.Error(t, (&ActivitySample{ActivityScore: -0.1}).Validate())
		require.NoError(t, (&ActivitySample{ActivityScore: 0.2, SaturationScore: 0.3}).Validate())

		require.Error(t, (&JobCheckpoint{ItemsDone: -1}).Validate())
		require.NoError(t, (&JobCheckpoint{ItemsDone: 10, ItemsTotal: 20}).Validate())

		require.Error(t, (&JobStatus{Name: ""}).Validate())
		require.Error(t, (&JobStatus{Name: "job1", MinRegime: Regime("BAD"), LockScope: LockScopeLocalDC}).Validate())
		require.Error(t, (&JobStatus{Name: "job1", MinRegime: RegimeIdle, LockScope: LockScope("bad")}).Validate())
		require.NoError(t, (&JobStatus{Name: "job1", MinRegime: RegimeIdle, LockScope: LockScopeLocalDC}).Validate())

		require.Error(t, (&SchedulerOverview{Regime: Regime("BAD")}).Validate())
		require.NoError(t, (&SchedulerOverview{Datacenter: "dc1", Regime: RegimeIdle}).Validate())
	})
}
