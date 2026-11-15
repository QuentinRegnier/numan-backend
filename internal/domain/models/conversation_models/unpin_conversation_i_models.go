package conversation_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// UnpinConversationInput valide l'action de désépingler une conversation.
type UnpinConversationInput struct {
	ConversationID int64 `json:"conversation_id" binding:"required"`
}

// Validate vérifie l'identifiant de conversation à désépingler.
func (p *UnpinConversationInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de désépinglage est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	return nil
}

type UnpinConversationOutput struct {
	InboxUpdateAt int64 `json:"inbox_update_at"`
}
