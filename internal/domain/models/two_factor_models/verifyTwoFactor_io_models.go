package two_factor_models

import (
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

func isSixDigitOTP(code string) bool {
	if len(code) != 6 {
		return false
	}
	for i := 0; i < 6; i++ {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}

// VerifyTwoFactorEmailInput valide la route POST /auth/verify-2fa/email
type VerifyTwoFactorEmailInput struct {
	PreAuthToken string `json:"pre_auth_token" binding:"required"`
	Code         string `json:"code" binding:"required,len=6,numeric"`
}

// Validate vérifie le jeton de pré-authentification et le format du code OTP à 6 chiffres.
func (p *VerifyTwoFactorEmailInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	if strings.TrimSpace(p.PreAuthToken) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le jeton de pré-authentification est obligatoire.", nil)
	}
	if !isSixDigitOTP(p.Code) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le code de vérification doit comporter exactement 6 chiffres.", nil)
	}
	return nil
}

// VerifyTwoFactorTOTPInput valide la route POST /auth/verify-2fa/totp
type VerifyTwoFactorTOTPInput struct {
	PreAuthToken string `json:"pre_auth_token" binding:"required"`
	Code         string `json:"code" binding:"required,len=6,numeric"`
}

// Validate vérifie le jeton de pré-authentification et le format du code TOTP à 6 chiffres.
func (p *VerifyTwoFactorTOTPInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	if strings.TrimSpace(p.PreAuthToken) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le jeton de pré-authentification est obligatoire.", nil)
	}
	if !isSixDigitOTP(p.Code) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le code TOTP doit comporter exactement 6 chiffres.", nil)
	}
	return nil
}

// VerifyTwoFactorPasskeyInput valide la route POST /auth/verify-2fa/passkey
type VerifyTwoFactorPasskeyInput struct {
	PreAuthToken        string                      `json:"pre_auth_token" binding:"required"`
	PasskeyAssertion    PasskeyAssertionResponse    `json:"passkey_assertion" binding:"omitempty"`
	PasskeyRegistration PasskeyRegistrationResponse `json:"passkey_registration" binding:"omitempty"`
}

// Validate vérifie le jeton de pré-authentification et la présence d'une assertion ou d'un enregistrement WebAuthn valide.
func (p *VerifyTwoFactorPasskeyInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	if strings.TrimSpace(p.PreAuthToken) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le jeton de pré-authentification est obligatoire.", nil)
	}
	hasAssertion := p.PasskeyAssertion != (PasskeyAssertionResponse{})
	hasRegistration := p.PasskeyRegistration.CredentialID != "" ||
		p.PasskeyRegistration.PublicKeyCOSE != "" ||
		p.PasskeyRegistration.AuthenticatorData != "" ||
		p.PasskeyRegistration.ClientDataJSON != "" ||
		len(p.PasskeyRegistration.Transports) > 0 ||
		p.PasskeyRegistration.DeviceName != ""

	if !hasAssertion && !hasRegistration {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Une assertion ou un enregistrement passkey est requis.", nil)
	}
	if hasAssertion {
		if err := p.PasskeyAssertion.Validate(); err != nil {
			return err
		}
	}
	if hasRegistration {
		if err := p.PasskeyRegistration.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// SendTwoFactorEmailInput permet de déclencher/renvoyer le code OTP par email lors d'un Login multi-méthodes
type SendTwoFactorEmailInput struct {
	PreAuthToken string `json:"pre_auth_token" binding:"required"`
}

// Validate vérifie la présence du jeton de pré-authentification.
func (p *SendTwoFactorEmailInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	if strings.TrimSpace(p.PreAuthToken) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le jeton de pré-authentification est obligatoire.", nil)
	}
	return nil
}

// VerifyTwoFactorOutput unifie la réponse finale de connexion/inscription après validation 2FA
type VerifyTwoFactorOutput struct {
	Flow        string                 `json:"flow"` // "login" ou "signup"
	UserID      int64                  `json:"user_id"`
	MasterToken string                 `json:"master_token"`
	JWT         string                 `json:"jwt"`
	ExpiresAt   int64                  `json:"expires_at"`
	Avatar      media_models.MediaView `json:"avatar,omitempty"`
	Message     string                 `json:"message"`
}
