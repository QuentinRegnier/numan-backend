package two_factor_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"

// VerifyTwoFactorEmailInput valide la route POST /auth/verify-2fa/email
type VerifyTwoFactorEmailInput struct {
	PreAuthToken string `json:"pre_auth_token" binding:"required"`
	Code         string `json:"code" binding:"required,len=6,numeric"`
}

// VerifyTwoFactorTOTPInput valide la route POST /auth/verify-2fa/totp
type VerifyTwoFactorTOTPInput struct {
	PreAuthToken string `json:"pre_auth_token" binding:"required"`
	Code         string `json:"code" binding:"required,len=6,numeric"`
}

// VerifyTwoFactorPasskeyInput valide la route POST /auth/verify-2fa/passkey
type VerifyTwoFactorPasskeyInput struct {
	PreAuthToken        string                      `json:"pre_auth_token" binding:"required"`
	PasskeyAssertion    PasskeyAssertionResponse    `json:"passkey_assertion" binding:"omitempty"`
	PasskeyRegistration PasskeyRegistrationResponse `json:"passkey_registration" binding:"omitempty"`
}

// SendTwoFactorEmailInput permet de déclencher/renvoyer le code OTP par email lors d'un Login multi-méthodes
type SendTwoFactorEmailInput struct {
	PreAuthToken string `json:"pre_auth_token" binding:"required"`
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
