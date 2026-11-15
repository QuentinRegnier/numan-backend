package conversation_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

type GetConversationsInput struct {
	ConversationIDs []int64 `json:"conversation_ids" binding:"required,min=1,max=50"`
}

// Validate vérifie les identifiants demandés pour la récupération en lot.
func (p *GetConversationsInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de récupération des conversations est nul.", nil)
	}
	if len(p.ConversationIDs) == 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Au moins un identifiant de conversation est requis.", nil)
	}
	if len(p.ConversationIDs) > 50 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Impossible de récupérer plus de 50 conversations à la fois.", nil)
	}
	for _, id := range p.ConversationIDs {
		if id <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Tous les identifiants de conversation doivent être strictement positifs.", nil)
		}
	}
	return nil
}

type GetInboxOutput struct {
	Conversations []InboxConversationView `json:"conversations"`
}
