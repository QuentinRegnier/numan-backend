package message_models

// CreateMessageInput valide les données entrantes.
type CreateMessageInput struct {
	ConversationID int64          `json:"conversation_id" binding:"required"`
	MessageType    int            `json:"message_type" binding:"required,min=0,max=10"`
	Content        string         `json:"content" binding:"omitempty,max=2200"`
	Attachments    map[string]any `json:"attachments" binding:"omitempty"`
	ThreadParentID int64          `json:"thread_parent_id" binding:"omitempty"` // NOUVEAU
}

type CreateMessageOutput struct {
	MessageID     int64 `json:"message_id"`
	InboxUpdateAt int64 `json:"inbox_update_at"`
}
