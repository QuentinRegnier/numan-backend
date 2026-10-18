package two_factor_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/security"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/email_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// SendTwoFactorEmailCode génère et expédie un nouveau code OTP par email pour une session PreAuth active.
// Permet à l'utilisateur qui a plusieurs méthodes 2FA de choisir l'Email à la volée lors du Login.
func SendTwoFactorEmailCode(ctx context.Context, input two_factor_models.SendTwoFactorEmailInput) error {
	preAuth, err := cache_service.GetPreAuthSession(ctx, input.PreAuthToken)
	if err != nil {
		return err
	}

	if !pkg.Exists(preAuth.AvailableMethods, variables.TwoFactorMethodEmail) && preAuth.Method != variables.TwoFactorMethodEmail {
		return numan_error.NewForbidden(numan_error.CodeForbidden, "La vérification par email n'est pas activée pour cette session.", nil)
	}

	var targetEmail, targetUsername string
	if preAuth.Flow == "signup" {
		targetEmail = preAuth.SignUpData.Email
		targetUsername = preAuth.SignUpData.Username
	} else {
		userPayload, errUser := loadUserByIDCascade(ctx, preAuth.UserID)
		if errUser != nil {
			return errUser
		}
		targetEmail = userPayload.Email
		targetUsername = userPayload.Username
	}

	rawCode, codeHash, errOTP := security.GenerateEmailOTP()
	if errOTP != nil {
		return numan_error.NewInternal(errOTP)
	}

	preAuth.CodeHash = codeHash
	if errSave := cache_service.SavePreAuthSession(ctx, preAuth); errSave != nil {
		return errSave
	}

	go func(targetID int64, email, username, code string) {
		bgCtx := context.Background()
		_ = email_service.DispatchEmail(
			bgCtx,
			targetID,
			email,
			"two_factor_code",
			"Votre code de sécurité Numan",
			map[string]any{
				"Username": username,
				"Code":     code,
			},
		)
	}(preAuth.UserID, targetEmail, targetUsername, rawCode)

	return nil
}
