package member_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type GetCommunityRequestsInput struct {
	ConversationID int64 `json:"conversation_id" binding:"required"`
	Limit          int64 `json:"limit" binding:"omitempty,min=1,max=50"`
	Offset         int64 `json:"offset" binding:"omitempty,min=0"`
}

// Validate vérifie les paramètres de pagination des candidatures en communauté.
func (p *GetCommunityRequestsInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload des demandes d'adhésion est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	if p.Limit < 0 || p.Limit > 50 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La limite doit être comprise entre 1 et 50.", nil)
	}
	if p.Offset < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'offset ne peut pas être négatif.", nil)
	}
	return nil
}

type GetCommunityRequestsOutput struct {
	Requests []auth_models.UserLiteView `json:"requests"`
}
