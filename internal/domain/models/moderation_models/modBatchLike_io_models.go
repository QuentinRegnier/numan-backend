package moderation_models

type ModBatchLikeInput struct {
	AuthorUserIDs []int64 `json:"author_user_ids" binding:"required,min=1,max=500"`
	Reason        string  `json:"reason,omitempty" binding:"omitempty,max=500"`
}

type ModBatchLikeOutput struct {
	EventID    int64 `json:"event_id"`
	UsersCount int   `json:"users_count"`
	QueuedAt   int64 `json:"queued_at"`
}
