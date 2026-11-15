package saved_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

type SaveActionInput struct {
	PostID int64 `json:"post_id" binding:"required"`
}

// Validate vérifie l'identifiant de publication à sauvegarder ou retirer des favoris.
func (p *SaveActionInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de sauvegarde est nul.", nil)
	}
	if p.PostID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la publication doit être strictement positif.", nil)
	}
	return nil
}
