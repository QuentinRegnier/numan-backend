package admin_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// TriggerSchedulerJobService déclenche manuellement un job planifié (localement ou via file Redis distante).
func TriggerSchedulerJobService(ctx context.Context, callerID int64, adminGrade int, input *admin_models.TriggerSchedulerJobInput) (admin_models.SchedulerActionOutput, error) {
	if callerID <= 0 {
		return admin_models.SchedulerActionOutput{}, numan_error.NewUnauthorized(numan_error.CodeUnauthorized, "Authentification requise.", nil)
	}
	if adminGrade != variables.UserGradeAdmin && adminGrade != variables.MemberRoleAdmin {
		return admin_models.SchedulerActionOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Privilèges d'administrateur requis.", nil)
	}
	if err := input.Validate(); err != nil {
		return admin_models.SchedulerActionOutput{}, err
	}

	st, queued, err := scheduler_service.TriggerJobGlobal(ctx, input.JobName, input.Force)
	if err != nil {
		return admin_models.SchedulerActionOutput{}, err
	}

	status := "executed"
	if queued {
		status = "queued_remote_trigger"
	}

	numan_log.Info(ctx).
		Int64("admin_id", callerID).
		Str("job", input.JobName).
		Bool("force", input.Force).
		Bool("queued_remote", queued).
		Msg("Déclenchement manuel d'un job scheduler par un administrateur.")

	return admin_models.SchedulerActionOutput{
		Status: status,
		Job:    st,
		Queued: queued,
	}, nil
}
