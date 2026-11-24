package admin_service

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

const defaultAdminPauseMinutes = 30

// PauseSchedulerService met le Scheduler en pause globale avec TTL auto-expirant et préemption coopérative immédiate.
func PauseSchedulerService(ctx context.Context, callerID int64, adminGrade int, input *admin_models.PauseSchedulerInput) (admin_models.SchedulerActionOutput, error) {
	if callerID <= 0 {
		return admin_models.SchedulerActionOutput{}, numan_error.NewUnauthorized(numan_error.CodeUnauthorized, "Authentification requise.", nil)
	}
	if adminGrade != variables.UserGradeAdmin && adminGrade != variables.MemberRoleAdmin {
		return admin_models.SchedulerActionOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Privilèges d'administrateur requis.", nil)
	}
	if input == nil {
		input = &admin_models.PauseSchedulerInput{}
	}
	if err := input.Validate(); err != nil {
		return admin_models.SchedulerActionOutput{}, err
	}

	ttlMin := input.TTLMinutes
	if ttlMin <= 0 {
		ttlMin = defaultAdminPauseMinutes
	}
	reason := input.Reason
	if reason == "" {
		reason = "admin_pause"
	}

	if err := scheduler_service.PauseGlobal(ctx, time.Duration(ttlMin)*time.Minute, reason); err != nil {
		return admin_models.SchedulerActionOutput{}, err
	}

	numan_log.Warn(ctx).
		Int64("admin_id", callerID).
		Int("ttl_minutes", ttlMin).
		Str("reason", reason).
		Msg("Mise en pause globale du scheduler par un administrateur.")

	return admin_models.SchedulerActionOutput{
		Status:     "paused",
		TTLMinutes: ttlMin,
		Reason:     reason,
	}, nil
}
