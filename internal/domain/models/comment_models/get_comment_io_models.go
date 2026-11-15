package comment_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type GetCommentsInput struct {
	PostID         int64   `form:"post_id" json:"post_id" binding:"required"`
	Limit          int64   `form:"limit,default=50" json:"limit"`
	Offset         int64   `form:"offset,default=0" json:"offset"`
	AfterScore     *int    `form:"after_score" json:"after_score"`
	AfterCreatedAt *string `form:"after_created_at" json:"after_created_at"`
	AfterID        *int64  `form:"after_id" json:"after_id"`
}

// Validate vérifie les paramètres de récupération paginée des commentaires d'un post.
func (p *GetCommentsInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres de récupération des commentaires sont nuls.", nil)
	}
	if p.PostID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la publication doit être strictement positif.", nil)
	}
	if p.Offset < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'offset de pagination doit être positif ou nul.", nil)
	}
	if p.Limit < 0 || p.Limit > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La limite de pagination doit être comprise entre 1 et 100.", nil)
	}
	if p.AfterID != nil && *p.AfterID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de curseur (after_id) doit être strictement positif.", nil)
	}
	return nil
}

// GetCommentOutput représente la réponse pour un ID spécifique
type GetCommentOutput struct {
	CommentID      int64                  `json:"comment_id"`
	Data           CommentPayload         `json:"data,omitempty"`
	AuthorUsername string                 `json:"author_username,omitempty"` // ✅ NOUVEAU : Le pseudo de l'auteur
	AuthorAvatar   media_models.MediaView `json:"author_avatar"`             // <-- NOUVEAU (Par valeur)
	Error          string                 `json:"numan_error,omitempty"`
}
