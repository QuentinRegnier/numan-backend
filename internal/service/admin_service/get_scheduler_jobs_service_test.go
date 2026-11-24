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

func TestGetSchedulerJobsService(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	eng := scheduler_service.NewEngine("dc-eu-1", scheduler_service.DefaultRegimeConfig(), nil)
	scheduler_service.RegisterAllDefaultJobs(eng)
	scheduler_service.SetDefault(eng)
	t.Cleanup(func() { scheduler_service.SetDefault(nil) })

	t.Run("rejects non-admin caller", func(t *testing.T) {
		_, err := admin_service.GetSchedulerJobsService(ctx, 1, variables.UserGradeUser, nil)
		require.Error(t, err)
		assert.Equal(t, http.StatusForbidden, err.(*numan_error.AppError).HTTPStatus)
	})

	t.Run("returns full scheduler overview for admin", func(t *testing.T) {
		ov, err := admin_service.GetSchedulerJobsService(ctx, 1, variables.UserGradeAdmin, &admin_models.GetSchedulerJobsInput{
			Datacenter: "dc-eu-1",
		})
		require.NoError(t, err)
		assert.Equal(t, "dc-eu-1", ov.Datacenter)
		assert.Len(t, ov.Jobs, 24)
	})
}
