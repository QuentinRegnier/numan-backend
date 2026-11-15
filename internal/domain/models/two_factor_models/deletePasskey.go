package two_factor_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

type DeletePasskeyInput struct {
	PasskeyID int64 `json:"passkey_id" binding:"required"`
}

// Validate vérifie que l'identifiant de passkey à supprimer est strictement positif.
func (p *DeletePasskeyInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	if p.PasskeyID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la passkey doit être strictement positif.", nil)
	}
	return nil
}
