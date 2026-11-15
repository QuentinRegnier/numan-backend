package notification_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// ReadNotificationsInput valide l'ID Snowflake jusqu'où l'utilisateur a lu ses notifications
type ReadNotificationsInput struct {
	ReadUpToID int64 `json:"read_up_to_id" binding:"required"`
}

// Validate vérifie le curseur de lecture des notifications.
func (p *ReadNotificationsInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload d'acquittement des notifications est nul.", nil)
	}
	if p.ReadUpToID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de notification lu doit être strictement positif.", nil)
	}
	return nil
}

type ReadNotificationsOutput struct {
	ActivityUpdateAt int64 `json:"activity_update_at"`
}
