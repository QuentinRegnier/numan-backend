package relation_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

type GetFriendsInput struct {
	TargetID int64 `json:"target_id" binding:"required"`
	Limit    int64 `json:"limit" binding:"omitempty,min=1,max=100"`
	Offset   int64 `json:"offset" binding:"omitempty,min=0"`
}

// Validate vérifie les paramètres de pagination et l'identifiant cible.
func (p *GetFriendsInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	if p.TargetID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant cible doit être strictement positif.", nil)
	}
	if p.Limit < 0 || p.Limit > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La limite doit être comprise entre 0 et 100.", nil)
	}
	if p.Offset < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'offset ne peut pas être négatif.", nil)
	}
	return nil
}

type GetFriendsOutput struct {
	Users []RelationUserView `json:"users"`
}
