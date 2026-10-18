package two_factor_service

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/security"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// VerifyTwoFactorTOTP valide une preuve 2FA par code d'application TOTP RFC 6238 (Login ou SignUp).
func VerifyTwoFactorTOTP(ctx context.Context, input two_factor_models.VerifyTwoFactorTOTPInput) (two_factor_models.VerifyTwoFactorOutput, error) {
	preAuth, err := cache_service.GetPreAuthSession(ctx, input.PreAuthToken)
	if err != nil {
		return two_factor_models.VerifyTwoFactorOutput{}, err
	}

	if !pkg.Exists(preAuth.AvailableMethods, variables.TwoFactorMethodTOTP) && preAuth.Method != variables.TwoFactorMethodTOTP {
		return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "La méthode Authenticator (TOTP) n'est pas autorisée pour cette session.", nil)
	}

	if preAuth.TempTOTPSecret == "" || !security.VerifyTOTP(preAuth.TempTOTPSecret, input.Code, time.Now().UTC()) {
		if errInc := cache_service.IncrementPreAuthAttempts(ctx, preAuth); errInc != nil {
			return two_factor_models.VerifyTwoFactorOutput{}, errInc
		}
		return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewUnauthorized(numan_error.CodeTwoFactorInvalid, "Code d'authentification invalide.", nil)
	}

	// Protection Anti-Rejeu TOTP sur la fenêtre de 90s
	isFresh, errReplay := cache_service.ConsumeTOTPCode(ctx, preAuth.UserID, input.Code)
	if errReplay != nil {
		return two_factor_models.VerifyTwoFactorOutput{}, errReplay
	}
	if !isFresh {
		return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewForbidden(numan_error.CodeTwoFactorInvalid, "Ce code TOTP a déjà été utilisé. Attendez le prochain cycle de 30 secondes.", nil)
	}

	_ = cache_service.DeletePreAuthSession(ctx, preAuth.Token)

	if preAuth.Flow == "signup" {
		return finalizeSignupAfterTwoFactor(ctx, preAuth, variables.TwoFactorMethodTOTP, nil)
	}

	userPayload, errUser := loadUserByIDCascade(ctx, preAuth.UserID)
	if errUser != nil {
		return two_factor_models.VerifyTwoFactorOutput{}, errUser
	}

	return finalizeLoginSession(ctx, userPayload, preAuth.LoginData, preAuth.IPAddresses)
}
