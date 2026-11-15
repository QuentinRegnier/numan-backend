package saved_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// GetSavedInput valide les paramètres de pagination dans l'URL (Query String)
type GetSavedInput struct {
	Limit           int64  `form:"limit,default=50" json:"limit" binding:"omitempty,min=1,max=100"`
	Offset          int64  `form:"offset,default=0" json:"offset" binding:"min=0"`
	BeforeCreatedAt *int64 `form:"before_created_at" json:"before_created_at"`
	BeforeID        *int64 `form:"before_id" json:"before_id"`
}

// Validate vérifie les paramètres de pagination pour la récupération des favoris.
func (p *GetSavedInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres de récupération des favoris sont nuls.", nil)
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
