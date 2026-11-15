package member_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// MuteMemberInput valide la requête pour muter ou démuter un participant.
// Aucun paramètre dans l'URL, tout est ici.
type MuteMemberInput struct {
	ConversationID int64 `json:"conversation_id" binding:"required"`
	TargetUserID   int64 `json:"target_user_id" binding:"required"`
	// Timestamp Unix en millisecondes. Si 0 = Démute immédiat.
	RestrictedUntil int64 `json:"restricted_until" binding:"min=0"`
}

// Validate vérifie les contraintes métier pour restreindre ou lever la restriction d'un membre.
func (p *MuteMemberInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de restriction du membre est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	if p.TargetUserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de l'utilisateur cible doit être strictement positif.", nil)
	}
	if p.RestrictedUntil < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le timestamp de restriction ne peut pas être négatif.", nil)
	}
	return nil
}
