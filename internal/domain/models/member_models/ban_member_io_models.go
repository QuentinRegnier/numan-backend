package member_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// BanMemberInput valide la demande de bannissement d'un membre.
type BanMemberInput struct {
	ConversationID int64 `json:"conversation_id" binding:"required"`
	TargetUserID   int64 `json:"target_user_id" binding:"required"`
}

// Validate vérifie les identifiants pour le bannissement d'un membre.
func (p *BanMemberInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de bannissement est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	if p.TargetUserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de l'utilisateur cible doit être strictement positif.", nil)
	}
	return nil
}

type BanMemberOutput struct {
	InboxUpdateAt int64 `json:"inbox_update_at"`
}
