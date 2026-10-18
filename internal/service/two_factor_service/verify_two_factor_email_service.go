package two_factor_service

import (
	"context"
	"crypto/hmac"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/security"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// VerifyTwoFactorEmail valide une preuve 2FA par code Email à 6 chiffres (Login ou SignUp).
func VerifyTwoFactorEmail(ctx context.Context, input two_factor_models.VerifyTwoFactorEmailInput) (two_factor_models.VerifyTwoFactorOutput, error) {
	preAuth, err := cache_service.GetPreAuthSession(ctx, input.PreAuthToken)
	if err != nil {
		return two_factor_models.VerifyTwoFactorOutput{}, err
	}

	if !pkg.Exists(preAuth.AvailableMethods, variables.TwoFactorMethodEmail) && preAuth.Method != variables.TwoFactorMethodEmail {
		return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "La méthode Email n'est pas autorisée pour cette session.", nil)
	}

	expectedHash := security.HashEmailOTP(input.Code)
	if preAuth.CodeHash == "" || !hmac.Equal([]byte(preAuth.CodeHash), []byte(expectedHash)) {
		if errInc := cache_service.IncrementPreAuthAttempts(ctx, preAuth); errInc != nil {
			return two_factor_models.VerifyTwoFactorOutput{}, errInc
		}
		return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewUnauthorized(numan_error.CodeTwoFactorInvalid, "Code de sécurité invalide.", nil)
	}

	_ = cache_service.DeletePreAuthSession(ctx, preAuth.Token)

	if preAuth.Flow == "signup" {
		return finalizeSignupAfterTwoFactor(ctx, preAuth, variables.TwoFactorMethodEmail, nil)
	}

	userPayload, errUser := loadUserByIDCascade(ctx, preAuth.UserID)
	if errUser != nil {
		return two_factor_models.VerifyTwoFactorOutput{}, errUser
	}

	return finalizeLoginSession(ctx, userPayload, preAuth.LoginData, preAuth.IPAddresses)
}
