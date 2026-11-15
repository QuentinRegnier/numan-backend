package relation_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

type GetBlockedInput struct {
	Limit  int64 `json:"limit" binding:"omitempty,min=1,max=100"`
	Offset int64 `json:"offset" binding:"omitempty,min=0"`
}

// Validate vérifie les paramètres de pagination.
func (p *GetBlockedInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	if p.Limit < 0 || p.Limit > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La limite doit être comprise entre 0 et 100.", nil)
	}
	if p.Offset < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'offset ne peut pas être négatif.", nil)
	}
	return nil
}

type GetBlockedOutput struct {
	Users []RelationUserView `json:"users"`
}
