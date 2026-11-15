package sync_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/notification_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// SyncActivityInput récupère l'état actuel du cache du téléphone de l'utilisateur.
type SyncActivityInput struct {
	ClientUpdatedAt int64 `json:"client_updated_at" binding:"min=0"`
	ReadUpToID      int64 `json:"read_up_to_id" binding:"min=0"` // Le Snowflake ID de la notif la plus récente que le client possède
}

// Validate vérifie les marqueurs temporels et de lecture pour le sync d'activité.
func (p *SyncActivityInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de synchronisation d'activité est nul.", nil)
	}
	if p.ClientUpdatedAt < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le timestamp client ne peut pas être négatif.", nil)
	}
	if p.ReadUpToID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de notification lu ne peut pas être négatif.", nil)
	}
	return nil
}

// SyncActivityOutput ne renvoie les notifications que si elles sont nouvelles.
type SyncActivityOutput struct {
	NeedUpdate    bool                                   `json:"need_update"`
	ServerUpdated int64                                  `json:"server_updated"`
	Notifications []notification_models.NotificationView `json:"notifications,omitempty"` // ✅ Remplacé par NotificationView
}
