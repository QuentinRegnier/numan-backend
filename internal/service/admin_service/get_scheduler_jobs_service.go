package admin_service

import (
	"context"
	"os"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// GetSchedulerJobsService retourne la vue consolidée du Scheduler et de tous ses jobs planifiés.
func GetSchedulerJobsService(ctx context.Context, callerID int64, adminGrade int, input *admin_models.GetSchedulerJobsInput) (admin_models.GetSchedulerJobsOutput, error) {
	if callerID <= 0 {
		return admin_models.GetSchedulerJobsOutput{}, numan_error.NewUnauthorized(numan_error.CodeUnauthorized, "Authentification requise.", nil)
	}
	if adminGrade != variables.UserGradeAdmin && adminGrade != variables.MemberRoleAdmin {
		return admin_models.GetSchedulerJobsOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Privilèges d'administrateur requis.", nil)
	}
	if input == nil {
		input = &admin_models.GetSchedulerJobsInput{}
	}
	if err := input.Validate(); err != nil {
		return admin_models.GetSchedulerJobsOutput{}, err
	}

	dc := input.Datacenter
	if dc == "" {
		dc = strings.TrimSpace(os.Getenv("DATACENTER_ID"))
	}
	ov := scheduler_service.GetJobsOverview(ctx, dc)

	numan_log.Info(ctx).
		Int64("admin_id", callerID).
		Str("datacenter", ov.Datacenter).
		Str("regime", string(ov.Regime)).
		Int("jobs_count", len(ov.Jobs)).
		Msg("Consultation administrative des jobs du scheduler.")

	return ov, nil
}
