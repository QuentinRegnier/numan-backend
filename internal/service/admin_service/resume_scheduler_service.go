package admin_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ResumeSchedulerService lève la pause globale du Scheduler dans Redis Core et sur l'instance locale.
func ResumeSchedulerService(ctx context.Context, callerID int64, adminGrade int) (admin_models.SchedulerActionOutput, error) {
	if callerID <= 0 {
		return admin_models.SchedulerActionOutput{}, numan_error.NewUnauthorized(numan_error.CodeUnauthorized, "Authentification requise.", nil)
	}
	if adminGrade != variables.UserGradeAdmin && adminGrade != variables.MemberRoleAdmin {
		return admin_models.SchedulerActionOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Privilèges d'administrateur requis.", nil)
	}

	if err := scheduler_service.ResumeGlobal(ctx); err != nil {
		return admin_models.SchedulerActionOutput{}, err
	}

	numan_log.Info(ctx).
		Int64("admin_id", callerID).
		Msg("Reprise globale du scheduler par un administrateur.")

	return admin_models.SchedulerActionOutput{
		Status: "resumed",
	}, nil
}
