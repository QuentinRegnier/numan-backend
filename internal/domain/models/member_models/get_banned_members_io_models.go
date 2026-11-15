package member_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// GetBannedMembersInput valide les paramètres du JSON pour récupérer la liste
type GetBannedMembersInput struct {
	ConversationID int64 `json:"conversation_id" binding:"required"`
	Limit          int64 `json:"limit" binding:"omitempty,min=1,max=100"`
	Offset         int64 `json:"offset" binding:"omitempty,min=0"`
}

// Validate vérifie les paramètres de pagination pour la liste des bannis.
func (p *GetBannedMembersInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload des membres bannis est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	if p.Limit < 0 || p.Limit > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La limite doit être comprise entre 1 et 100.", nil)
	}
	if p.Offset < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'offset ne peut pas être négatif.", nil)
	}
	return nil
}

// BannedUserView enrichit le profil classique avec la date du bannissement
type BannedUserView struct {
	auth_models.UserLiteView
	BanDate int64 `json:"ban_date"` // Timestamp Unix (int64) correspondant à UpdatedAt du membre
}

type GetBannedMembersOutput struct {
	BannedUsers []BannedUserView `json:"banned_users"`
}
