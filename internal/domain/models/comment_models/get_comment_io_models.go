package comment_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"

type GetCommentsInput struct {
	PostID         int64   `form:"post_id" json:"post_id" binding:"required"`
	Limit          int64   `form:"limit,default=50" json:"limit"`
	Offset         int64   `form:"offset,default=0" json:"offset"`
	AfterScore     *int    `form:"after_score" json:"after_score"`
	AfterCreatedAt *string `form:"after_created_at" json:"after_created_at"`
	AfterID        *int64  `form:"after_id" json:"after_id"`
}

// GetCommentOutput représente la réponse pour un ID spécifique
type GetCommentOutput struct {
	CommentID      int64                  `json:"comment_id"`
	Data           CommentPayload         `json:"data,omitempty"`
	AuthorUsername string                 `json:"author_username,omitempty"` // ✅ NOUVEAU : Le pseudo de l'auteur
	AuthorAvatar   media_models.MediaView `json:"author_avatar"`             // <-- NOUVEAU (Par valeur)
	Error          string                 `json:"numan_error,omitempty"`
}
