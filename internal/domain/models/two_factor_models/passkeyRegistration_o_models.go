package two_factor_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type PasskeyRegistrationResponse struct {
	CredentialID      string   `json:"credential_id" binding:"required"`
	PublicKeyCOSE     string   `json:"public_key_cose" binding:"required"`
	AuthenticatorData string   `json:"authenticator_data" binding:"required"`
	ClientDataJSON    string   `json:"client_data_json" binding:"required"`
	Transports        []string `json:"transports" binding:"omitempty"`
	DeviceName        string   `json:"device_name" binding:"omitempty,max=64"`
}

// Validate vérifie que tous les champs WebAuthn d'enregistrement d'une passkey sont présents et valides.
func (p *PasskeyRegistrationResponse) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La réponse d'enregistrement passkey est nulle.", nil)
	}
	if strings.TrimSpace(p.CredentialID) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de passkey (credential_id) est obligatoire.", nil)
	}
	if strings.TrimSpace(p.PublicKeyCOSE) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La clé publique COSE est obligatoire.", nil)
	}
	if strings.TrimSpace(p.AuthenticatorData) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les données d'authentificateur sont obligatoires.", nil)
	}
	if strings.TrimSpace(p.ClientDataJSON) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les données client JSON sont obligatoires.", nil)
	}
	if utf8.RuneCountInString(p.DeviceName) > 64 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom de l'appareil ne peut pas dépasser 64 caractères.", nil)
	}
	return nil
}
