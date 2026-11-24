package admin_service_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/admin_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPauseSchedulerService(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()
	scheduler_service.SetDefault(nil)

	t.Run("rejects invalid ttl_minutes", func(t *testing.T) {
		_, err := admin_service.PauseSchedulerService(ctx, 1, variables.UserGradeAdmin, &admin_models.PauseSchedulerInput{
			TTLMinutes: 5000,
		})
		require.Error(t, err)
		assert.Equal(t, http.StatusBadRequest, err.(*numan_error.AppError).HTTPStatus)
	})

	t.Run("pauses scheduler globally with default ttl when omitted", func(t *testing.T) {
		out, err := admin_service.PauseSchedulerService(ctx, 1, variables.UserGradeAdmin, &admin_models.PauseSchedulerInput{
			Reason: "db_migration",
		})
		require.NoError(t, err)
		assert.Equal(t, "paused", out.Status)
		assert.Equal(t, 30, out.TTLMinutes)
		assert.Equal(t, "db_migration", out.Reason)

		ov := scheduler_service.GetJobsOverview(ctx, "dc-eu-1")
		assert.True(t, ov.Paused)
		assert.Equal(t, "db_migration", ov.PauseReason)
	})
}
