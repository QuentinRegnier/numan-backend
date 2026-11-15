package conversation_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// LeaveConversationInput contient les données nécessaires pour quitter ou supprimer une conversation
type LeaveConversationInput struct {
	ConversationID int64 `json:"conversation_id" binding:"required"`
	NewOwnerID     int64 `json:"new_owner_id" binding:"omitempty"`
}

// Validate vérifie les identifiants pour quitter une conversation.
func (p *LeaveConversationInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de départ de conversation est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	if p.NewOwnerID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du nouveau propriétaire ne peut pas être négatif.", nil)
	}
	return nil
}

type LeaveConversationOutput struct {
	InboxUpdateAt int64 `json:"conversation_update_at" binding:"required"`
}
