package admin_service_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/admin_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTriggerSchedulerJobService(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	eng := scheduler_service.NewEngine("dc-eu-1", scheduler_service.DefaultRegimeConfig(), nil)
	eng.Register(scheduler_service.JobDefinition{
		Name:      "demo_job",
		Category:  "hygiene",
		MinRegime: scheduler_models.RegimeDeepIdle,
		LockScope: scheduler_models.LockScopeLocalDC,
		Fn: func(_ context.Context, cp scheduler_models.JobCheckpoint, _ bool) (scheduler_models.JobCheckpoint, bool, error) {
			return cp, true, nil
		},
	})
	scheduler_service.SetDefault(eng)
	t.Cleanup(func() { scheduler_service.SetDefault(nil) })

	t.Run("rejects invalid job_name and unknown job", func(t *testing.T) {
		_, err := admin_service.TriggerSchedulerJobService(ctx, 1, variables.UserGradeAdmin, &admin_models.TriggerSchedulerJobInput{JobName: "BAD!"})
		require.Error(t, err)
		assert.Equal(t, http.StatusBadRequest, err.(*numan_error.AppError).HTTPStatus)

		_, err = admin_service.TriggerSchedulerJobService(ctx, 1, variables.UserGradeAdmin, &admin_models.TriggerSchedulerJobInput{JobName: "unknown_job"})
		require.Error(t, err)
		assert.Equal(t, http.StatusNotFound, err.(*numan_error.AppError).HTTPStatus)
	})

	t.Run("executes known job for admin", func(t *testing.T) {
		out, err := admin_service.TriggerSchedulerJobService(ctx, 1, variables.UserGradeAdmin, &admin_models.TriggerSchedulerJobInput{
			JobName: "demo_job",
			Force:   true,
		})
		require.NoError(t, err)
		assert.Equal(t, "executed", out.Status)
		require.NotNil(t, out.Job)
		assert.Equal(t, "completed", out.Job.Status)
	})
}
