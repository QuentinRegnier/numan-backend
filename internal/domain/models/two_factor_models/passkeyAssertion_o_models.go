package two_factor_models

import (
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type PasskeyAssertionResponse struct {
	CredentialID      string `json:"credential_id" binding:"required"`
	AuthenticatorData string `json:"authenticator_data" binding:"required"`
	ClientDataJSON    string `json:"client_data_json" binding:"required"`
	Signature         string `json:"signature" binding:"required"`
	UserHandle        string `json:"user_handle,omitempty"`
}

// Validate vérifie que tous les champs WebAuthn d'assertion d'une passkey sont présents.
func (p *PasskeyAssertionResponse) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La réponse d'assertion passkey est nulle.", nil)
	}
	if strings.TrimSpace(p.CredentialID) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de passkey (credential_id) est obligatoire.", nil)
	}
	if strings.TrimSpace(p.AuthenticatorData) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les données d'authentificateur sont obligatoires.", nil)
	}
	if strings.TrimSpace(p.ClientDataJSON) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les données client JSON sont obligatoires.", nil)
	}
	if strings.TrimSpace(p.Signature) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La signature WebAuthn est obligatoire.", nil)
	}
	return nil
}
