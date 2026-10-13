package message_models

// MessagePayload correspond exactement au schéma Postgres messaging.messages
type MessagePayload struct {
	Attachments       map[string]any `json:"attachments" bson:"attachments"` // JSONB polymorphe
	Content           string         `json:"content" bson:"content"`
	ID                int64          `json:"id" bson:"id"`
	ConversationID    int64          `json:"conversation_id" bson:"conversation_id"`
	SenderID          int64          `json:"sender_id" bson:"sender_id"`
	MessageType       int            `json:"message_type" bson:"message_type"`
	CreatedAt         int64          `json:"created_at" bson:"created_at"`
	UpdatedAt         int64          `json:"updated_at" bson:"updated_at"`
	Visibility        bool           `json:"visibility" bson:"visibility"`
	AllowNewReactions bool           `json:"allow_new_reactions" bson:"allow_new_reactions"`
}
