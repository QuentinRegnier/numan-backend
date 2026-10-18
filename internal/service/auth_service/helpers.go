package auth_service

import (
	"context"
	"fmt"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/security"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/email_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// resolveConfiguredTwoFactorMethods détermine toutes les méthodes 2FA (1: Email, 2: TOTP, 3: Passkey)
// configurées et utilisables par l'utilisateur au moment de sa connexion.
func resolveConfiguredTwoFactorMethods(ctx context.Context, user auth_models.UserPayload) ([]int, []two_factor_models.PasskeyPayload) {
	var methods []int

	// 1. Vérification Email (Bit 0 actif ou fallback si TwoFactorMethod == 1)
	if (user.TwoFactorMethod&variables.TwoFactorBitEmail) != 0 || user.TwoFactorMethod == variables.TwoFactorMethodEmail {
		methods = append(methods, variables.TwoFactorMethodEmail)
	}

	// 2. Vérification TOTP (Secret TOTP présent)
	if user.TOTPSecret != "" && ((user.TwoFactorMethod&variables.TwoFactorBitTOTP) != 0 || user.TwoFactorMethod == variables.TwoFactorMethodTOTP) {
		methods = append(methods, variables.TwoFactorMethodTOTP)
	}

	// 3. Vérification Passkeys (Clés matérielles FIDO2 enregistrées)
	passkeys, _ := object_cache_service.GetUserPasskeysCascade(ctx, user.ID)
	if len(passkeys) > 0 {
		methods = append(methods, variables.TwoFactorMethodPasskey)
	}

	// Sécurité : si TwoFactorEnabled est true mais qu'aucun bit précis n'a matché, on autorise l'Email par défaut
	if len(methods) == 0 && user.Email != "" {
		methods = append(methods, variables.TwoFactorMethodEmail)
	}

	return methods, passkeys
}

// initiateLoginTwoFactorChallenge prépare la PreAuthSession en RAM (L1) en exposant TOUTES les méthodes
// configurées par l'utilisateur pour qu'il puisse choisir librement laquelle utiliser au Login.
func initiateLoginTwoFactorChallenge(
	ctx context.Context,
	userPayload auth_models.UserPayload,
	input auth_models.LoginInput,
	ipAddresses []string,
) (auth_models.LoginResponse, error) {
	availableMethods, passkeys := resolveConfiguredTwoFactorMethods(ctx, userPayload)

	preAuthToken, errToken := pkg.GenerateToken(userPayload.ID, input.FirebaseInstallationID, 300)
	if errToken != nil {
		return auth_models.LoginResponse{}, numan_error.NewInternal()
	}

	preAuthSession := auth_models.PreAuthSession{
		Token:            preAuthToken,
		Flow:             "login",
		UserID:           userPayload.ID,
		Method:           userPayload.TwoFactorMethod,
		AvailableMethods: availableMethods,
		LoginData:        input,
		IPAddresses:      ipAddresses,
		Attempts:         0,
		CreatedAt:        domain.NowMillis(),
	}

	response := auth_models.LoginResponse{
		RequiresTwoFactor: true,
		TwoFactorMethod:   availableMethods[0],
		AvailableMethods:  availableMethods,
		PreAuthToken:      preAuthToken,
		UserID:            userPayload.ID,
		Message:           "Double authentification requise. Veuillez choisir votre méthode de vérification.",
	}

	// 1. Préparation TOTP si configuré
	if pkg.Exists(availableMethods, variables.TwoFactorMethodTOTP) {
		preAuthSession.TempTOTPSecret = userPayload.TOTPSecret
	}

	// 2. Préparation Passkey (WebAuthn Challenge + allowCredentials) si configurée
	if pkg.Exists(availableMethods, variables.TwoFactorMethodPasskey) {
		challenge, errChal := security.GenerateWebAuthnChallenge()
		if errChal != nil {
			numan_log.Error(ctx).Err(errChal).Int64("user_id", userPayload.ID).Msg("Échec génération challenge WebAuthn au Login")
			return auth_models.LoginResponse{}, numan_error.NewInternal()
		}
		preAuthSession.WebAuthnChallenge = challenge

		allowCredentials := make([]map[string]any, 0, len(passkeys))
		for _, pk := range passkeys {
			allowCredentials = append(allowCredentials, map[string]any{
				"type":       "public-key",
				"id":         pk.CredentialID,
				"transports": pk.Transports,
			})
		}

		response.PasskeyOptions = map[string]any{
			"challenge":         challenge,
			"rp_id":             security.GetRPID(),
			"user_verification": "required",
			"allow_credentials": allowCredentials,
		}
	}

	// 3. Préparation Email OTP si configuré
	if pkg.Exists(availableMethods, variables.TwoFactorMethodEmail) {
		rawCode, codeHash, errOTP := security.GenerateEmailOTP()
		if errOTP != nil {
			numan_log.Error(ctx).Err(errOTP).Int64("user_id", userPayload.ID).Msg("Échec génération OTP Email au Login")
			return auth_models.LoginResponse{}, numan_error.NewInternal()
		}
		preAuthSession.CodeHash = codeHash

		// Si l'Email est la seule méthode configurée, on expédie immédiatement l'email
		if len(availableMethods) == 1 {
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
			}(userPayload.ID, userPayload.Email, userPayload.Username, rawCode)
			response.Message = "Un code de vérification a été envoyé par email."
		}
	}

	if errCache := cache_service.SavePreAuthSession(ctx, preAuthSession); errCache != nil {
		return auth_models.LoginResponse{}, errCache
	}

	return response, nil
}

// initiateSignupTwoFactorChallenge prépare la session PreAuth en RAM (L1) lors du SignUp.
func initiateSignupTwoFactorChallenge(
	ctx context.Context,
	input auth_models.SignUpInput,
	ipAddress string,
) (auth_models.SignUpResponse, error) {
	pendingUserID := pkg.GenerateID()

	preAuthToken, errToken := pkg.GenerateToken(pendingUserID, input.FirebaseInstallationID, 300)
	if errToken != nil {
		return auth_models.SignUpResponse{}, numan_error.NewInternal()
	}

	preAuthSession := auth_models.PreAuthSession{
		Token:            preAuthToken,
		Flow:             "signup",
		UserID:           pendingUserID,
		Method:           input.TwoFactorMethod,
		AvailableMethods: []int{input.TwoFactorMethod},
		SignUpData:       input,
		IPAddresses:      []string{ipAddress},
		Attempts:         0,
		CreatedAt:        domain.NowMillis(),
	}

	response := auth_models.SignUpResponse{
		RequiresTwoFactor: true,
		TwoFactorMethod:   input.TwoFactorMethod,
		PreAuthToken:      preAuthToken,
		UserID:            pendingUserID,
	}

	switch input.TwoFactorMethod {
	case variables.TwoFactorMethodEmail:
		rawCode, codeHash, errOTP := security.GenerateEmailOTP()
		if errOTP != nil {
			numan_log.Error(ctx).Err(errOTP).Int64("user_id", pendingUserID).Msg("Échec génération OTP Email au SignUp")
			return auth_models.SignUpResponse{}, numan_error.NewInternal()
		}
		preAuthSession.CodeHash = codeHash

		if errCache := cache_service.SavePreAuthSession(ctx, preAuthSession); errCache != nil {
			return auth_models.SignUpResponse{}, errCache
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
		}(pendingUserID, input.Email, input.Username, rawCode)

		response.Message = "Un code de vérification a été envoyé par email pour finaliser votre inscription."
		return response, nil

	case variables.TwoFactorMethodTOTP:
		totpSecret, errSecret := security.GenerateTOTPSecret()
		if errSecret != nil {
			numan_log.Error(ctx).Err(errSecret).Int64("user_id", pendingUserID).Msg("Échec génération secret TOTP au SignUp")
			return auth_models.SignUpResponse{}, numan_error.NewInternal()
		}
		qrURI := security.BuildTOTPURI("Numan", input.Username, totpSecret)
		preAuthSession.TempTOTPSecret = totpSecret

		if errCache := cache_service.SavePreAuthSession(ctx, preAuthSession); errCache != nil {
			return auth_models.SignUpResponse{}, errCache
		}

		response.TOTPSecret = totpSecret
		response.TOTPQRCodeURI = qrURI
		response.Message = "Veuillez configurer votre application d'authentification et saisir le code à 6 chiffres."
		return response, nil

	case variables.TwoFactorMethodPasskey:
		challenge, errChal := security.GenerateWebAuthnChallenge()
		if errChal != nil {
			numan_log.Error(ctx).Err(errChal).Int64("user_id", pendingUserID).Msg("Échec génération challenge WebAuthn au SignUp")
			return auth_models.SignUpResponse{}, numan_error.NewInternal()
		}
		preAuthSession.WebAuthnChallenge = challenge

		if errCache := cache_service.SavePreAuthSession(ctx, preAuthSession); errCache != nil {
			return auth_models.SignUpResponse{}, errCache
		}

		response.PasskeyOptions = map[string]any{
			"challenge": challenge,
			"rp": map[string]any{
				"name": "Numan",
				"id":   security.GetRPID(),
			},
			"user": map[string]any{
				"id":           fmt.Sprintf("%d", pendingUserID),
				"name":         input.Username,
				"display_name": input.Username,
			},
			"pub_key_cred_params": []map[string]any{
				{"type": "public-key", "alg": -7}, // ES256 (P-256)
				{"type": "public-key", "alg": -8}, // EdDSA (Ed25519)
			},
			"authenticator_selection": map[string]any{
				"user_verification": "required",
			},
		}
		response.Message = "Veuillez enregistrer votre Passkey pour finaliser votre inscription."
		return response, nil

	default:
		return auth_models.SignUpResponse{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Méthode de double authentification non reconnue.", nil)
	}
}
