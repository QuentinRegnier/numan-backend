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

func TestRemoteControlDistributedFlow(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	// Simuler un api_node sans instance locale du scheduler
	scheduler_service.SetDefault(nil)
	t.Cleanup(func() { scheduler_service.SetDefault(nil) })

	require.NoError(t, scheduler_service.PauseGlobal(ctx, 5*time.Minute, "deploy"))
	ov := scheduler_service.GetJobsOverview(ctx, "dc-eu-1")
	assert.True(t, ov.Paused)
	assert.Equal(t, "deploy", ov.PauseReason)

	// Déclenchement refusé sans force pendant la pause
	_, _, err := scheduler_service.TriggerJobGlobal(ctx, "job_remote", false)
	require.Error(t, err)

	// Déclenchement accepté en file distante avec force=true
	st, queued, err := scheduler_service.TriggerJobGlobal(ctx, "job_remote", true)
	require.NoError(t, err)
	assert.True(t, queued)
	assert.Equal(t, "queued_remote_trigger", st.Status)

	require.NoError(t, scheduler_service.ResumeGlobal(ctx))

	// Simuler le scheduler_node consommant le déclenchement distant
	ran := false
	cfg := scheduler_service.DefaultRegimeConfig()
	cfg.DownshiftHysteresis = 0
	eng := scheduler_service.NewEngine("dc-eu-1", cfg, nil)
	eng.Register(scheduler_service.JobDefinition{
		Name:      "job_remote",
		Category:  "hygiene",
		MinRegime: scheduler_models.RegimeDeepIdle,
		LockScope: scheduler_models.LockScopeLocalDC,
		Fn: func(_ context.Context, cp scheduler_models.JobCheckpoint, _ bool) (scheduler_models.JobCheckpoint, bool, error) {
			ran = true
			return cp, true, nil
		},
	})
	assert.True(t, eng.Tick(ctx))
	assert.True(t, ran)
}
