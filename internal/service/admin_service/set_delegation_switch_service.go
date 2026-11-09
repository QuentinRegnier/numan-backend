package admin_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// SetDelegationSwitchService active ou désactive l'interrupteur de délégation administrative vers les Super-Modérateurs.
func SetDelegationSwitchService(ctx context.Context, adminGrade int, enabled bool) (admin_models.AdminDelegationSwitchOutput, error) {
	if adminGrade != variables.UserGradeAdmin {
		return admin_models.AdminDelegationSwitchOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Seul un Administrateur peut modifier l'interrupteur de délégation.", nil)
	}
	variables.ModerationAdminDelegationEnabled = enabled
	nowMs := domain.NowMillis()

	numan_log.Info(ctx).
		Bool("delegation_enabled", enabled).
		Msg("Interrupteur de délégation administrative de modération mis à jour.")

	return admin_models.AdminDelegationSwitchOutput{
		Enabled:   enabled,
		UpdatedAt: nowMs,
	}, nil
}
