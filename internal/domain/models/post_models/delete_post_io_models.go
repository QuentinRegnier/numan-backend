package post_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

type DeletePostInput struct {
	PostID int64 `json:"post_id" binding:"required"`
}

// Validate vérifie l'identifiant de publication pour la suppression.
func (p *DeletePostInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de suppression de publication est nul.", nil)
	}
	if p.PostID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la publication doit être strictement positif.", nil)
	}
	return nil
}
