package admin_service

import (
	"context"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service/infra_telemetry_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// GetTelemetryOverviewService retourne le snapshot unifié de télémétrie d'un datacenter pour l'Admin Panel.
func GetTelemetryOverviewService(ctx context.Context, callerID int64, adminGrade int, input *admin_models.GetTelemetryOverviewInput) (*admin_models.GetTelemetryOverviewOutput, error) {
	if callerID <= 0 {
		return nil, numan_error.NewUnauthorized(numan_error.CodeUnauthorized, "Authentification requise.", nil)
	}
	if adminGrade != variables.UserGradeAdmin && adminGrade != variables.MemberRoleAdmin {
		return nil, numan_error.NewForbidden(numan_error.CodeForbidden, "Privilèges d'administrateur requis.", nil)
	}
	if input == nil {
		input = &admin_models.GetTelemetryOverviewInput{}
	}
	if err := input.Validate(); err != nil {
		return nil, err
	}

	snap, err := infra_telemetry_service.GetOrComputeTelemetrySnapshot(ctx, input.Datacenter)
	if err != nil {
		return nil, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeInternalError, "Impossible de récupérer le snapshot de télémétrie datacenter.", err)
	}
	if snap == nil {
		return nil, numan_error.NewAppError(http.StatusServiceUnavailable, numan_error.CodeServiceUnavailable, "Aucun snapshot de télémétrie disponible pour ce datacenter.", nil)
	}

	numan_log.Info(ctx).
		Int64("admin_id", callerID).
		Str("datacenter", snap.Datacenter).
		Str("regime", string(snap.SchedulerRegime)).
		Msg("Consultation administrative de la télémétrie datacenter.")

	return snap, nil
}
