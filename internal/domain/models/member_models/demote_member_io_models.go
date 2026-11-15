package member_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// DemoteMemberInput valide la demande de destitution d'un administrateur.
type DemoteMemberInput struct {
	ConversationID int64 `json:"conversation_id" binding:"required"`
	TargetUserID   int64 `json:"target_user_id" binding:"required"`
}

// Validate vérifie les identifiants pour destituer un administrateur.
func (p *DemoteMemberInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de destitution est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	if p.TargetUserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de l'utilisateur cible doit être strictement positif.", nil)
	}
	return nil
}

type DemoteMemberOutput struct {
	InboxUpdateAt int64 `json:"inbox_update_at"`
}
