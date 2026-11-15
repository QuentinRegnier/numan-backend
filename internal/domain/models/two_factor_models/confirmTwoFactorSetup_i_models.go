package two_factor_models

import (
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type ConfirmTwoFactorSetupInput struct {
	SetupToken          string                       `json:"setup_token" binding:"required"`
	Code                string                       `json:"code" binding:"omitempty,len=6,numeric"`
	PasskeyRegistration *PasskeyRegistrationResponse `json:"passkey_registration" binding:"omitempty"`
}

// Validate vérifie le jeton de configuration ainsi que la preuve 2FA fournie (code à 6 chiffres ou passkey).
func (p *ConfirmTwoFactorSetupInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	if strings.TrimSpace(p.SetupToken) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le jeton de configuration est obligatoire.", nil)
	}
	if p.Code == "" && p.PasskeyRegistration == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Un code de vérification ou une passkey est requis.", nil)
	}
	if p.Code != "" && !isSixDigitOTP(p.Code) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le code de vérification doit comporter exactement 6 chiffres.", nil)
	}
	if p.PasskeyRegistration != nil {
		if err := p.PasskeyRegistration.Validate(); err != nil {
			return err
		}
	}
	return nil
}
