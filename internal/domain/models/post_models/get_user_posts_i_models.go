package post_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// GetUserPostsInput valide les paramètres de la requête de profil
type GetUserPostsInput struct {
	TargetUserID    int64  `json:"user_id" form:"user_id" binding:"required"`
	Limit           int64  `json:"limit" form:"limit"`
	Offset          int64  `json:"offset" form:"offset"`
	Force           bool   `json:"force" form:"force"`
	BeforeCreatedAt *int64 `json:"before_created_at" form:"before_created_at"`
	BeforeID        *int64 `json:"before_id" form:"before_id"`
}

// Validate vérifie les paramètres de pagination et d'identifiant utilisateur.
func (p *GetUserPostsInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres de récupération des publications sont nuls.", nil)
	}
	if p.TargetUserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de l'utilisateur cible doit être strictement positif.", nil)
	}
	if p.Offset < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'offset de pagination doit être positif ou nul.", nil)
	}
	if p.Limit < 0 || p.Limit > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La limite de pagination doit être comprise entre 1 et 100.", nil)
	}
	if p.BeforeID != nil && *p.BeforeID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de curseur (before_id) doit être strictement positif.", nil)
	}
	return nil
}
