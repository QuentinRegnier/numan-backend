package two_factor_service

import (
	"context"
	"crypto/hmac"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/security"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : CONFIRMATION D'ENRÔLEMENT 2FA
// ############################################################################

// ConfirmTwoFactorSetup valide la preuve d'enrôlement et active la méthode dans le bitmask de l'utilisateur.
func ConfirmTwoFactorSetup(ctx context.Context, userID int64, input two_factor_models.ConfirmTwoFactorSetupInput) error {
	session, err := cache_service.GetPreAuthSession(ctx, input.SetupToken)
	if err != nil {
		return err
	}

	if session.Flow != "setup" || session.UserID != userID {
		return numan_error.NewForbidden(numan_error.CodeForbidden, "Session de configuration invalide.", nil)
	}

	userPayload, errUser := loadUserByIDCascade(ctx, userID)
	if errUser != nil {
		return errUser
	}

	nowMs := domain.NowMillis()

	switch session.Method {
	case variables.TwoFactorMethodEmail:
		if input.Code == "" {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le code à 6 chiffres est requis.", nil)
		}
		expectedHash := security.HashEmailOTP(input.Code)
		if session.CodeHash == "" || !hmac.Equal([]byte(session.CodeHash), []byte(expectedHash)) {
			if errInc := cache_service.IncrementPreAuthAttempts(ctx, session); errInc != nil {
				return errInc
			}
			return numan_error.NewUnauthorized(numan_error.CodeTwoFactorInvalid, "Code de confirmation invalide.", nil)
		}
		userPayload.EmailVerified = true

	case variables.TwoFactorMethodTOTP:
		if input.Code == "" {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le code TOTP à 6 chiffres est requis.", nil)
		}
		if session.TempTOTPSecret == "" || !security.VerifyTOTP(session.TempTOTPSecret, input.Code, time.Now().UTC()) {
			if errInc := cache_service.IncrementPreAuthAttempts(ctx, session); errInc != nil {
				return errInc
			}
			return numan_error.NewUnauthorized(numan_error.CodeTwoFactorInvalid, "Code TOTP invalide.", nil)
		}
		userPayload.TOTPSecret = session.TempTOTPSecret

	case variables.TwoFactorMethodPasskey:
		if input.PasskeyRegistration == nil {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les données d'enregistrement de la Passkey sont manquantes.", nil)
		}

		encodedPubKey, signCount, errReg := security.VerifyPasskeyRegistration(
			session.WebAuthnChallenge,
			input.PasskeyRegistration.ClientDataJSON,
			input.PasskeyRegistration.AuthenticatorData,
			input.PasskeyRegistration.PublicKeyCOSE,
		)
		if errReg != nil {
			if errInc := cache_service.IncrementPreAuthAttempts(ctx, session); errInc != nil {
				return errInc
			}
			return numan_error.NewUnauthorized(numan_error.CodeTwoFactorInvalid, "Enregistrement de la Passkey invalide.", errReg)
		}

		deviceName := pkg.CleanStr(input.PasskeyRegistration.DeviceName)
		if deviceName == "" {
			deviceName = session.DeviceName
		}
		if deviceName == "" {
			deviceName = "Passkey"
		}

		newPasskey := two_factor_models.PasskeyPayload{
			ID:              pkg.GenerateID(),
			UserID:          userID,
			CredentialID:    pkg.CleanStr(input.PasskeyRegistration.CredentialID),
			PublicKey:       encodedPubKey,
			AttestationType: "none",
			SignCount:       signCount,
			DeviceName:      deviceName,
			Transports:      input.PasskeyRegistration.Transports,
			CreatedAt:       nowMs,
			UpdatedAt:       nowMs,
		}

		_ = object_cache_service.SetPasskeyInObjectCache(ctx, newPasskey)
		if errQueuePk := redis.EnqueueDB(ctx, newPasskey.ID, userID, redis.EntityPasskey, redis.ActionCreate, newPasskey, redis.TargetAll); errQueuePk != nil {
			return numan_error.NewInternal(errQueuePk)
		}

	default:
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Méthode inconnue.", nil)
	}

	userPayload.TwoFactorEnabled = true
	userPayload.TwoFactorMethod |= methodToBit(session.Method)
	userPayload.UpdatedAt = nowMs

	_ = cache_service.DeletePreAuthSession(ctx, session.Token)

	if errQueue := redis.EnqueueDB(ctx, userPayload.ID, userPayload.ID, redis.EntityUser, redis.ActionUpdate, userPayload, redis.TargetAll); errQueue != nil {
		return numan_error.NewInternal(errQueue)
	}

	return nil
}
