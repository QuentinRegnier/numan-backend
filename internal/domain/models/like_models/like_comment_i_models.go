package like_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// LikeCommentInput centralise toute la donnée de l'action pour un commentaire.
type LikeCommentInput struct {
	CommentID int64  `json:"comment_id" binding:"required"`
	Action    string `json:"action" binding:"required,oneof=like unlike"`
}

// Validate vérifie l'identifiant du commentaire et l'action demandée.
func (p *LikeCommentInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de like de commentaire est nul.", nil)
	}
	if p.CommentID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du commentaire doit être strictement positif.", nil)
	}
	if p.Action != "like" && p.Action != "unlike" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'action doit valoir 'like' ou 'unlike'.", nil)
	}
	return nil
}
