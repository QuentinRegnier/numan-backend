package admin_service_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/admin_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResumeSchedulerService(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()
	scheduler_service.SetDefault(nil)

	require.NoError(t, scheduler_service.PauseGlobal(ctx, 10*time.Minute, "maintenance"))

	t.Run("rejects non-admin caller", func(t *testing.T) {
		_, err := admin_service.ResumeSchedulerService(ctx, 1, variables.UserGradeUser)
		require.Error(t, err)
		assert.Equal(t, http.StatusForbidden, err.(*numan_error.AppError).HTTPStatus)
	})

	t.Run("resumes paused scheduler for admin", func(t *testing.T) {
		out, err := admin_service.ResumeSchedulerService(ctx, 1, variables.UserGradeAdmin)
		require.NoError(t, err)
		assert.Equal(t, "resumed", out.Status)

		ov := scheduler_service.GetJobsOverview(ctx, "dc-eu-1")
		assert.False(t, ov.Paused)
	})
}
