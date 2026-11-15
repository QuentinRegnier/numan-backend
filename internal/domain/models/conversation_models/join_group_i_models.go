package conversation_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// JoinGroupInput valide la demande de l'utilisateur pour rejoindre un groupe.
type JoinGroupInput struct {
	ConversationID int64 `json:"conversation_id" binding:"required"`
	InviteMsgID    int64 `json:"invite_msg_id" binding:"omitempty"` // Requis si la conversation est un groupe privé
	External       bool  `json:"external" binding:"omitempty"`
	Internal       bool  `json:"internal" binding:"omitempty"`
}

// Validate vérifie la cohérence des modes d'accès et identifiants pour rejoindre une conversation.
func (p *JoinGroupInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload pour rejoindre la conversation est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	if p.InviteMsgID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du message d'invitation ne peut pas être négatif.", nil)
	}
	if p.Internal && p.External {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les modes d'accès interne et externe sont mutuellement exclusifs.", nil)
	}
	return nil
}

type JoinGroupOutput struct {
	InboxUpdateAt int64 `json:"inbox_update_at"`
}
