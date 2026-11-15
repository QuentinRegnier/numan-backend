package auth_models

import (
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type LoginInput struct {
	Email                  string                 `json:"email" binding:"required,email" example:"john@numan.com"`
	PasswordHash           string                 `json:"password_hash" binding:"required" example:"hashed_secret_123"`
	DeviceInfo             map[string]interface{} `json:"device_info" swaggertype:"object"`
	FirebaseInstallationID string                 `json:"firebase_installation_id" binding:"required" example:"eyJhbGciOiJIUzI1Ni..."`
}

// Validate vérifie que les identifiants de connexion sont valides et non vides.
func (p *LoginInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de connexion est nul.", nil)
	}
	if !isValidEmail(p.Email) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'adresse email est invalide.", nil)
	}
	if strings.TrimSpace(p.PasswordHash) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le mot de passe est obligatoire.", nil)
	}
	if strings.TrimSpace(p.FirebaseInstallationID) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant d'installation Firebase est obligatoire.", nil)
	}
	return nil
}

type LoginResponse struct {
	RequiresTwoFactor bool   `json:"requires_two_factor"`
	TwoFactorMethod   int    `json:"two_factor_method,omitempty"`
	AvailableMethods  []int  `json:"available_methods,omitempty"` // Liste des méthodes configurées (ex: [1, 2, 3])
	PreAuthToken      string `json:"pre_auth_token,omitempty"`
	PasskeyOptions    any    `json:"passkey_options,omitempty"`
	UserID            int64  `json:"user_id,omitempty" example:"42"`
	MasterToken       string `json:"master_token,omitempty"`
	JWT               string `json:"jwt,omitempty" example:"eyJhbGciOiJIUzI1Ni..."`
	ExpiresAt         int64  `json:"expires_at,omitempty"`
	Message           string `json:"message" example:"Login successful"`
}
