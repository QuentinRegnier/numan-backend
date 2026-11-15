package conversation_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// ReadReceiptInput valide les données envoyées par l'application
type ReadReceiptInput struct {
	ConversationID int64 `json:"conversation_id" binding:"required"`
}

// Validate vérifie l'identifiant de conversation pour l'accusé de lecture.
func (p *ReadReceiptInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload d'accusé de lecture est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	return nil
}

// ReadReceiptOutput renvoie le timestamp de synchronisation de l'inbox
type ReadReceiptOutput struct {
	InboxUpdateAt int64 `json:"inbox_updated_at"`
}
