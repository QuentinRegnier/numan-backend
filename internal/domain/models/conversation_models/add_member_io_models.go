package conversation_models

import (
	"fmt"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// AddMemberInput valide la demande d'ajout de membres dans un groupe
type AddMemberInput struct {
	ConversationID int64   `json:"conversation_id" binding:"required"`
	ParticipantIDs []int64 `json:"participant_ids" binding:"required,min=1,max=100"`
}

// Validate vérifie les contraintes métier de l'ajout de participants.
func (p *AddMemberInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload d'ajout de membres est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	if len(p.ParticipantIDs) == 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Au moins un participant est requis.", nil)
	}
	if len(p.ParticipantIDs) > variables.MaxAddMemberCount {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, fmt.Sprintf("Impossible d'ajouter plus de %d participants à la fois.", variables.MaxAddMemberCount), nil)
	}
	for _, id := range p.ParticipantIDs {
		if id <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Tous les identifiants de participants doivent être strictement positifs.", nil)
		}
	}
	return nil
}

type AddMemberOutput struct {
	AddedUserIDs    []int64 `json:"added_user_ids"`    // Membres ajoutés automatiquement
	InvitedUserIDs  []int64 `json:"invited_user_ids"`  // Membres à qui une invitation a été envoyée
	RejectedUserIDs []int64 `json:"rejected_user_ids"` // Membres refusés par manque de droits de communication
	MessageIDs      []int64 `json:"message_ids"`       // IDs des messages d'invitation générés (vide si aucun)
	ConversationIDs []int64 `json:"conversation_ids"`  // IDs des conversations MP correspondantes (vide si aucun)
	InboxUpdateAt   int64   `json:"inbox_update_at"`
}
