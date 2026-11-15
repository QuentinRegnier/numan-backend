package auth_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

type DeleteSessionInput struct {
	SessionID int64 `json:"session_id" binding:"required"`
}

// Validate vérifie que l'identifiant de session est strictement positif.
func (p *DeleteSessionInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	if p.SessionID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de session doit être strictement positif.", nil)
	}
	return nil
}
