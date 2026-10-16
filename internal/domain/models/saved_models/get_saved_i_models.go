package saved_models

// GetSavedInput valide les paramètres de pagination dans l'URL (Query String)
type GetSavedInput struct {
	Limit           int64  `form:"limit,default=50" json:"limit" binding:"min=1,max=100"`
	Offset          int64  `form:"offset,default=0" json:"offset" binding:"min=0"`
	BeforeCreatedAt *int64 `form:"before_created_at" json:"before_created_at"`
	BeforeID        *int64 `form:"before_id" json:"before_id"`
}
