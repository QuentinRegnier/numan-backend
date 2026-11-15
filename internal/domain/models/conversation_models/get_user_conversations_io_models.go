package conversation_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

type GetUserConversationsInput struct {
	Limit  int64 `form:"limit,default=50"`
	Offset int64 `form:"offset,default=0"`
	Force  bool  `form:"force"`
}

// Validate vérifie les paramètres de pagination de l'inbox utilisateur.
func (p *GetUserConversationsInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de récupération de l'inbox est nul.", nil)
	}
	if p.Limit < 0 || p.Limit > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La limite doit être comprise entre 1 et 100.", nil)
	}
	if p.Offset < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'offset ne peut pas être négatif.", nil)
	}
	return nil
}

type GetUserInboxOutput struct {
	Conversations []InboxConversationView `json:"conversations"`
}
