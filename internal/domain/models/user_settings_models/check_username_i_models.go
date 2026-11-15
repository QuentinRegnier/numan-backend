package user_settings_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

type CheckUsernameInput struct {
	Username string `json:"username" binding:"required,min=3,max=30,alphanum"`
}

// Validate vérifie le format et la longueur du nom d'utilisateur (3 à 30 caractères alphanumériques ASCII).
func (p *CheckUsernameInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	if !isValidUsername(p.Username) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom d'utilisateur doit contenir entre 3 et 30 caractères alphanumériques ASCII.", nil)
	}
	return nil
}
