package two_factor_service

import (
	"context"
	"fmt"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/security"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/email_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : INITIALISATION DE CONFIGURATION 2FA (SETUP)
// ############################################################################

// SetupTwoFactor initie l'enrôlement d'une méthode 2FA (1: Email, 2: TOTP, 3: Passkey) en RAM L1.
func SetupTwoFactor(ctx context.Context, userID int64, input two_factor_models.SetupTwoFactorInput) (two_factor_models.SetupTwoFactorOutput, error) {
	userPayload, errUser := loadUserByIDCascade(ctx, userID)
	if errUser != nil {
		return two_factor_models.SetupTwoFactorOutput{}, errUser
	}

	setupToken, errToken := pkg.GenerateToken(userID, "2fa_setup", 300)
	if errToken != nil {
		return two_factor_models.SetupTwoFactorOutput{}, numan_error.NewInternal()
	}

	session := auth_models.PreAuthSession{
		Token:      setupToken,
		Flow:       "setup",
		UserID:     userID,
		Method:     input.Method,
		DeviceName: pkg.CleanStr(input.DeviceName),
		Attempts:   0,
		CreatedAt:  domain.NowMillis(),
	}

	output := two_factor_models.SetupTwoFactorOutput{
		SetupToken: setupToken,
		Method:     input.Method,
	}

	switch input.Method {
	case variables.TwoFactorMethodEmail:
		rawCode, codeHash, errOTP := security.GenerateEmailOTP()
		if errOTP != nil {
			return two_factor_models.SetupTwoFactorOutput{}, numan_error.NewInternal(errOTP)
		}
		session.CodeHash = codeHash

		if errSave := cache_service.SavePreAuthSession(ctx, session); errSave != nil {
			return two_factor_models.SetupTwoFactorOutput{}, errSave
		}

		go func(targetID int64, email, username, code string) {
			bgCtx := context.Background()
			_ = email_service.DispatchEmail(
				bgCtx,
				targetID,
				email,
				"two_factor_code",
				"Activation de votre double authentification Numan",
				map[string]any{
					"Username": username,
					"Code":     code,
				},
			)
		}(userPayload.ID, userPayload.Email, userPayload.Username, rawCode)

		output.Message = "Un code de confirmation vous a été envoyé par email."
		return output, nil

	case variables.TwoFactorMethodTOTP:
		totpSecret, errSecret := security.GenerateTOTPSecret()
		if errSecret != nil {
			return two_factor_models.SetupTwoFactorOutput{}, numan_error.NewInternal(errSecret)
		}
		qrURI := security.BuildTOTPURI("Numan", userPayload.Username, totpSecret)
		session.TempTOTPSecret = totpSecret

		if errSave := cache_service.SavePreAuthSession(ctx, session); errSave != nil {
			return two_factor_models.SetupTwoFactorOutput{}, errSave
		}

		output.TOTPSecret = totpSecret
		output.TOTPQRCodeURI = qrURI
		output.Message = "Scannez le QR Code et validez avec le code à 6 chiffres."
		return output, nil

	case variables.TwoFactorMethodPasskey:
		challenge, errChal := security.GenerateWebAuthnChallenge()
		if errChal != nil {
			return two_factor_models.SetupTwoFactorOutput{}, numan_error.NewInternal(errChal)
		}
		session.WebAuthnChallenge = challenge

		if errSave := cache_service.SavePreAuthSession(ctx, session); errSave != nil {
			return two_factor_models.SetupTwoFactorOutput{}, errSave
		}

		output.PasskeyOptions = map[string]any{
			"challenge": challenge,
			"rp": map[string]any{
				"name": "Numan",
				"id":   security.GetRPID(),
			},
			"user": map[string]any{
				"id":           fmt.Sprintf("%d", userPayload.ID),
				"name":         userPayload.Username,
				"display_name": userPayload.Username,
			},
			"pub_key_cred_params": []map[string]any{
				{"type": "public-key", "alg": -7},
				{"type": "public-key", "alg": -8},
			},
			"authenticator_selection": map[string]any{
				"user_verification": "required",
			},
		}
		output.Message = "Veuillez enregistrer votre nouvelle Passkey sur votre appareil."
		return output, nil

	default:
		return two_factor_models.SetupTwoFactorOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Méthode 2FA non supportée.", nil)
	}
}
