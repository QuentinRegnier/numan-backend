package security_models

import (
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type RefreshMasterInput struct {
	UserID      int64  `json:"id_user" binding:"required"`
	MasterToken string `json:"master_token" binding:"required"` // L'ancien MasterToken
	Username    string `json:"username" binding:"required"`     // Le username de l'utilisateur
}

// Validate vérifie les invariants métier de la requête de rotation du MasterToken.
func (p *RefreshMasterInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	if p.UserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur doit être strictement positif.", nil)
	}
	if strings.TrimSpace(p.MasterToken) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le jeton maître est obligatoire.", nil)
	}
	if strings.TrimSpace(p.Username) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom d'utilisateur est obligatoire.", nil)
	}
	return nil
}

type RefreshMasterResponse struct {
	MasterToken string `json:"master_token"`
	Token       string `json:"token"` // Le nouveau JWT
	Message     string `json:"message"`
}
