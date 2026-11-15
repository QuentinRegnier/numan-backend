package like_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// LikePostInput centralise toute la donnée de l'action.
type LikePostInput struct {
	PostID int64  `json:"post_id" binding:"required"`
	Action string `json:"action" binding:"required,oneof=like unlike"`
}

// Validate vérifie l'identifiant du post et l'action demandée.
func (p *LikePostInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de like de publication est nul.", nil)
	}
	if p.PostID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la publication doit être strictement positif.", nil)
	}
	if p.Action != "like" && p.Action != "unlike" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'action doit valoir 'like' ou 'unlike'.", nil)
	}
	return nil
}
