package member_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// UnbanMembersInput gère le débannissement par lots
type UnbanMembersInput struct {
	ConversationID int64   `json:"conversation_id" binding:"required"`
	TargetUserIDs  []int64 `json:"target_user_ids" binding:"required,min=1,max=100"`
}

// Validate vérifie les identifiants du débannissement en lot.
func (p *UnbanMembersInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de débannissement est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	if len(p.TargetUserIDs) == 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Au moins un utilisateur cible est requis.", nil)
	}
	if len(p.TargetUserIDs) > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Impossible de débannir plus de 100 utilisateurs à la fois.", nil)
	}
	for _, id := range p.TargetUserIDs {
		if id <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Tous les identifiants cibles doivent être strictement positifs.", nil)
		}
	}
	return nil
}

type UnbanMembersOutput struct {
	InboxUpdateAt int64 `json:"inbox_update_at"`
}
