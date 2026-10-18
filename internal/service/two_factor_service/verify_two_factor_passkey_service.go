package two_factor_service

import (
	"context"
	"reflect"

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

// VerifyTwoFactorPasskey valide une preuve cryptographique FIDO2/WebAuthn (Assertion au Login, Registration au SignUp).
func VerifyTwoFactorPasskey(ctx context.Context, input two_factor_models.VerifyTwoFactorPasskeyInput) (two_factor_models.VerifyTwoFactorOutput, error) {
	preAuth, err := cache_service.GetPreAuthSession(ctx, input.PreAuthToken)
	if err != nil {
		return two_factor_models.VerifyTwoFactorOutput{}, err
	}

	if !pkg.Exists(preAuth.AvailableMethods, variables.TwoFactorMethodPasskey) && preAuth.Method != variables.TwoFactorMethodPasskey {
		return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "La méthode Passkey n'est pas autorisée pour cette session.", nil)
	}

	// --- PARCOURS 1 : INSCRIPTION (SIGNUP AVEC ENREGISTREMENT DE PASSKEY) ---
	if preAuth.Flow == "signup" {
		if reflect.DeepEqual(input.PasskeyRegistration, two_factor_models.PasskeyRegistrationResponse{}) {
			return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les données d'enregistrement de la Passkey sont requises.", nil)
		}

		encodedPubKey, signCount, errReg := security.VerifyPasskeyRegistration(
			preAuth.WebAuthnChallenge,
			input.PasskeyRegistration.ClientDataJSON,
			input.PasskeyRegistration.AuthenticatorData,
			input.PasskeyRegistration.PublicKeyCOSE,
		)
		if errReg != nil {
			if errInc := cache_service.IncrementPreAuthAttempts(ctx, preAuth); errInc != nil {
				return two_factor_models.VerifyTwoFactorOutput{}, errInc
			}
			return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewUnauthorized(numan_error.CodeTwoFactorInvalid, "Échec de validation de la Passkey.", errReg)
		}

		nowMs := domain.NowMillis()
		deviceName := pkg.CleanStr(input.PasskeyRegistration.DeviceName)
		if deviceName == "" {
			deviceName = "Clé principale"
		}

		newPasskey := two_factor_models.PasskeyPayload{
			ID:              pkg.GenerateID(),
			UserID:          preAuth.UserID,
			CredentialID:    pkg.CleanStr(input.PasskeyRegistration.CredentialID),
			PublicKey:       encodedPubKey,
			AttestationType: "none",
			SignCount:       signCount,
			DeviceName:      deviceName,
			Transports:      input.PasskeyRegistration.Transports,
			CreatedAt:       nowMs,
			UpdatedAt:       nowMs,
		}

		_ = cache_service.DeletePreAuthSession(ctx, preAuth.Token)
		return finalizeSignupAfterTwoFactor(ctx, preAuth, variables.TwoFactorMethodPasskey, &newPasskey)
	}

	// --- PARCOURS 2 : CONNEXION (LOGIN AVEC ASSERTION DE PASSKEY) ---
	if reflect.DeepEqual(input.PasskeyAssertion, two_factor_models.PasskeyAssertionResponse{}) {
		return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'assertion de signature Passkey est requise.", nil)
	}

	passkeyPayload, errPk := object_cache_service.GetPasskeyByCredentialIDCascade(ctx, input.PasskeyAssertion.CredentialID)
	if errPk != nil || passkeyPayload.ID == 0 || passkeyPayload.UserID != preAuth.UserID {
		if errInc := cache_service.IncrementPreAuthAttempts(ctx, preAuth); errInc != nil {
			return two_factor_models.VerifyTwoFactorOutput{}, errInc
		}
		return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewUnauthorized(numan_error.CodeTwoFactorInvalid, "Passkey non reconnue pour ce compte.", nil)
	}

	newSignCount, isCloned, errAssert := security.VerifyPasskeyAssertion(
		preAuth.WebAuthnChallenge,
		passkeyPayload.PublicKey,
		passkeyPayload.SignCount,
		input.PasskeyAssertion.ClientDataJSON,
		input.PasskeyAssertion.AuthenticatorData,
		input.PasskeyAssertion.Signature,
	)
	if isCloned {
		_ = cache_service.DeletePreAuthSession(ctx, preAuth.Token)
		return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewForbidden(numan_error.CodePasskeyCloneAlert, "Alerte de sécurité : Anomalie de compteur détectée sur votre Passkey (suspicion de clonage).", errAssert)
	}
	if errAssert != nil {
		if errInc := cache_service.IncrementPreAuthAttempts(ctx, preAuth); errInc != nil {
			return two_factor_models.VerifyTwoFactorOutput{}, errInc
		}
		return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewUnauthorized(numan_error.CodeTwoFactorInvalid, "Signature Passkey invalide.", errAssert)
	}

	// Mise à jour du SignCount en L1 et Write-Behind L2/L3
	passkeyPayload.SignCount = newSignCount
	passkeyPayload.UpdatedAt = domain.NowMillis()
	_ = object_cache_service.SetPasskeyInObjectCache(ctx, passkeyPayload)
	_ = redis.EnqueueDB(ctx, passkeyPayload.ID, passkeyPayload.UserID, redis.EntityPasskey, redis.ActionUpdate, passkeyPayload, redis.TargetAll)

	_ = cache_service.DeletePreAuthSession(ctx, preAuth.Token)

	userPayload, errUser := loadUserByIDCascade(ctx, preAuth.UserID)
	if errUser != nil {
		return two_factor_models.VerifyTwoFactorOutput{}, errUser
	}

	return finalizeLoginSession(ctx, userPayload, preAuth.LoginData, preAuth.IPAddresses)
}
