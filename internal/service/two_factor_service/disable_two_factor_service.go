package two_factor_service

import (
	"context"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : DÉSACTIVATION DE LA DOUBLE AUTHENTIFICATION (2FA)
// ############################################################################

// DisableTwoFactor désactive une méthode spécifique (1, 2, 3) ou toute la 2FA (0) après vérification du mot de passe.
func DisableTwoFactor(ctx context.Context, userID int64, input two_factor_models.DisableTwoFactorInput) error {
	userPayload, errUser := loadUserByIDCascade(ctx, userID)
	if errUser != nil {
		return errUser
	}

	if strings.TrimSpace(userPayload.PasswordHash) != strings.TrimSpace(input.PasswordHash) {
		return numan_error.NewUnauthorized(numan_error.CodeUnauthorized, "Mot de passe incorrect.", nil)
	}

	if input.Method == 0 {
		userPayload.TwoFactorEnabled = false
		userPayload.TwoFactorMethod = 0
		userPayload.TOTPSecret = ""
	} else {
		bit := methodToBit(input.Method)
		userPayload.TwoFactorMethod &^= bit
		if input.Method == variables.TwoFactorMethodTOTP {
			userPayload.TOTPSecret = ""
		}
		if userPayload.TwoFactorMethod == 0 {
			userPayload.TwoFactorEnabled = false
		}
	}

	userPayload.UpdatedAt = domain.NowMillis()

	if errQueue := redis.EnqueueDB(ctx, userPayload.ID, userPayload.ID, redis.EntityUser, redis.ActionUpdate, userPayload, redis.TargetAll); errQueue != nil {
		return numan_error.NewInternal(errQueue)
	}

	return nil
}
