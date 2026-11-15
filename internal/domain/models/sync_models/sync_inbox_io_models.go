package sync_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type SyncInboxInput struct {
	ClientUpdatedAt int64 `json:"client_updated_at"` // Timestamp en millisecondes fourni par le client
}

// Validate vérifie le timestamp client pour la synchronisation de l'inbox.
func (p *SyncInboxInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de synchronisation inbox est nul.", nil)
	}
	if p.ClientUpdatedAt < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le timestamp client_updated_at ne peut pas être négatif.", nil)
	}
	return nil
}

type SyncInboxOutput struct {
	NeedUpdate    bool                                        `json:"need_update"`
	ServerUpdated int64                                       `json:"server_updated"`
	Conversations []conversation_models.InboxConversationView `json:"conversations,omitempty"` // Renvoyé uniquement si NeedUpdate = true
}
