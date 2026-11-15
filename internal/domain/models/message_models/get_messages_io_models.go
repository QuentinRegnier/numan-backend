package message_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// GetMessagesInput valide les paramètres de la requête pour charger l'historique
type GetMessagesInput struct {
	ConversationID int64  `form:"conversation_id" binding:"required"`
	Limit          int64  `form:"limit,default=50" binding:"max=100"`
	OffsetID       int64  `form:"offset_id,default=0"` // L'ID du message à partir duquel on scrolle
	Direction      string `form:"direction,default=top" binding:"oneof=top bottom"`
}

// Validate vérifie les paramètres de pagination et de direction de l'historique des messages.
func (p *GetMessagesInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de récupération des messages est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	if p.Limit < 0 || p.Limit > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La limite doit être comprise entre 1 et 100.", nil)
	}
	if p.OffsetID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de décalage ne peut pas être négatif.", nil)
	}
	switch p.Direction {
	case "", "top", "bottom":
	default:
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La direction doit être 'top' ou 'bottom'.", nil)
	}
	return nil
}

type GetMessagesOutput struct {
	Messages []MessageView `json:"messages"` // ✅ REMPLACÉ : On utilise la MessageView
}
