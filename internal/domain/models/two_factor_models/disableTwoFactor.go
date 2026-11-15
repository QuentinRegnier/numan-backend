package two_factor_models

import (
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

type DisableTwoFactorInput struct {
	PasswordHash string `json:"password_hash" binding:"required"`
	Method       int    `json:"method" binding:"omitempty,oneof=0 1 2 3"` // 0 = Désactiver toute la 2FA, 1 = Email, 2 = TOTP, 3 = Passkey
}

// Validate vérifie la présence du mot de passe hashé et la validité de la méthode ciblée.
func (p *DisableTwoFactorInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	if strings.TrimSpace(p.PasswordHash) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le mot de passe est obligatoire.", nil)
	}
	switch p.Method {
	case variables.TwoFactorMethodNone, variables.TwoFactorMethodEmail, variables.TwoFactorMethodTOTP, variables.TwoFactorMethodPasskey:
		return nil
	default:
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La méthode 2FA sélectionnée est invalide.", nil)
	}
}
