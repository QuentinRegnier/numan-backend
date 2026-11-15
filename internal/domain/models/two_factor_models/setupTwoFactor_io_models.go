package two_factor_models

import (
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

type SetupTwoFactorInput struct {
	Method     int    `json:"method" binding:"required,oneof=1 2 3"`
	DeviceName string `json:"device_name" binding:"omitempty,max=64"`
}

// Validate vérifie que la méthode 2FA demandée et le nom d'appareil sont valides.
func (p *SetupTwoFactorInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	switch p.Method {
	case variables.TwoFactorMethodEmail, variables.TwoFactorMethodTOTP, variables.TwoFactorMethodPasskey:
	default:
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La méthode 2FA sélectionnée est invalide (1: Email, 2: TOTP, 3: Passkey).", nil)
	}
	if utf8.RuneCountInString(p.DeviceName) > 64 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom de l'appareil ne peut pas dépasser 64 caractères.", nil)
	}
	return nil
}

type SetupTwoFactorOutput struct {
	SetupToken     string `json:"setup_token"`
	Method         int    `json:"method"`
	TOTPSecret     string `json:"totp_secret,omitempty"`
	TOTPQRCodeURI  string `json:"totp_qr_uri,omitempty"`
	PasskeyOptions any    `json:"passkey_options,omitempty"`
	Message        string `json:"message"`
}
