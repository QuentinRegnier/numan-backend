package admin_service_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/admin_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTelemetryOverviewService(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	t.Run("rejects unauthorized and non-admin callers", func(t *testing.T) {
		_, err := admin_service.GetTelemetryOverviewService(ctx, 0, variables.UserGradeAdmin, nil)
		require.Error(t, err)
		assert.Equal(t, http.StatusUnauthorized, err.(*numan_error.AppError).HTTPStatus)

		_, err = admin_service.GetTelemetryOverviewService(ctx, 42, variables.UserGradeUser, nil)
		require.Error(t, err)
		assert.Equal(t, http.StatusForbidden, err.(*numan_error.AppError).HTTPStatus)
	})

	t.Run("rejects invalid datacenter identifier", func(t *testing.T) {
		_, err := admin_service.GetTelemetryOverviewService(ctx, 1, variables.UserGradeAdmin, &admin_models.GetTelemetryOverviewInput{
			Datacenter: "../bad",
		})
		require.Error(t, err)
		assert.Equal(t, http.StatusBadRequest, err.(*numan_error.AppError).HTTPStatus)
	})

	t.Run("returns live datacenter telemetry snapshot for admin", func(t *testing.T) {
		snap, err := admin_service.GetTelemetryOverviewService(ctx, 1, variables.UserGradeAdmin, &admin_models.GetTelemetryOverviewInput{
			Datacenter: "dc-eu-1",
		})
		require.NoError(t, err)
		require.NotNil(t, snap)
		assert.Equal(t, "dc-eu-1", snap.Datacenter)
	})
}
