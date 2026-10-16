package post_models

// GetUserPostsInput valide les paramètres de la requête de profil
type GetUserPostsInput struct {
	TargetUserID    int64  `json:"user_id" form:"user_id" binding:"required"`
	Limit           int64  `json:"limit" form:"limit"`
	Offset          int64  `json:"offset" form:"offset"`
	Force           bool   `json:"force" form:"force"`
	BeforeCreatedAt *int64 `json:"before_created_at" form:"before_created_at"`
	BeforeID        *int64 `json:"before_id" form:"before_id"`
}
