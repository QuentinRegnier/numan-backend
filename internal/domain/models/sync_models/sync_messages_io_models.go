package sync_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// SyncMessagesInput valide la requête POST entrante.
// Zéro paramètre d'URL : l'ID de conversation et le timestamp sont dans le JSON.
type SyncMessagesInput struct {
	ConversationID int64 `json:"conversation_id" binding:"required"`
	SinceMs        int64 `json:"since_ms" binding:"min=0"`
}

// Validate vérifie la validité de l'identifiant de conversation et du timestamp de synchronisation.
func (p *SyncMessagesInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de synchronisation des messages est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation est invalide.", nil)
	}
	if p.SinceMs < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le timestamp since_ms ne peut pas être négatif.", nil)
	}
	return nil
}

// SyncMessagesOutput structure la réponse avec le DTO MessageView classique
type SyncMessagesOutput struct {
	Messages []message_models.MessageView `json:"messages"`
}
