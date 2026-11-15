package comment_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

type DeleteCommentInput struct {
	CommentID int64 `json:"comment_id" binding:"required"`
}

// Validate vérifie l'identifiant du commentaire à supprimer.
func (p *DeleteCommentInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de suppression de commentaire est nul.", nil)
	}
	if p.CommentID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du commentaire doit être strictement positif.", nil)
	}
	return nil
}
