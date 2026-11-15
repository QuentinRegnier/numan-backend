package conversation_models

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

type CreateConversationInput struct {
	Type           int                  `json:"type" binding:"oneof=0 1 2 3"`
	Title          string               `json:"title" binding:"omitempty,max=100"`
	Settings       ConversationSettings `json:"settings"` // Remplacement de Laws
	ParticipantIDs []int64              `json:"participant_ids" binding:"required,min=1,max=100"`
}

// Validate vérifie les règles métier de création d'une conversation.
func (p *CreateConversationInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de création de conversation est nul.", nil)
	}
	if p.Type < variables.ConversationTypeDirect || p.Type > variables.ConversationTypeCommunityPub {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le type de conversation est invalide.", nil)
	}
	title := strings.TrimSpace(p.Title)
	if utf8.RuneCountInString(title) > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le titre de la conversation ne peut pas dépasser 100 caractères.", nil)
	}
	if len(p.ParticipantIDs) == 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Au moins un participant est requis.", nil)
	}
	if len(p.ParticipantIDs) > variables.MaxAddMemberCount {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, fmt.Sprintf("Le nombre de participants ne peut pas dépasser %d.", variables.MaxAddMemberCount), nil)
	}
	for _, id := range p.ParticipantIDs {
		if id <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Tous les identifiants de participants doivent être strictement positifs.", nil)
		}
	}
	if err := p.Settings.Validate(); err != nil {
		return err
	}
	return nil
}

type CreateConversationOutput struct {
	ConversationID  int64   `json:"conversation_id"`
	AddedUserIDs    []int64 `json:"added_user_ids,omitempty"`    // Membres ajoutés automatiquement (pour les groupes)
	InvitedUserIDs  []int64 `json:"invited_user_ids,omitempty"`  // Membres invités (pour les groupes)
	RejectedUserIDs []int64 `json:"rejected_user_ids,omitempty"` // Membres refusés (pour les groupes)
	MessageIDs      []int64 `json:"message_ids,omitempty"`       // IDs des messages d'invitation générés (pour les groupes)
	ConversationIDs []int64 `json:"conversation_ids,omitempty"`  // IDs des conversations MP correspondantes (pour les groupes)
	InboxUpdateAt   int64   `json:"inbox_update_at"`
}
