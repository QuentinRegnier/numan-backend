package notification_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// GetNotificationsInput valide les paramètres de la requête pour charger l'historique
type GetNotificationsInput struct {
	Limit  int64 `form:"limit,default=50" binding:"max=100"`
	Offset int64 `form:"offset,default=0" binding:"min=0"`
	Force  bool  `form:"force"` // Si true, force la purge du cache L1 et la lecture L2
}

// Validate vérifie les paramètres de pagination des notifications.
func (p *GetNotificationsInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload des notifications est nul.", nil)
	}
	if p.Limit < 0 || p.Limit > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La limite doit être comprise entre 1 et 100.", nil)
	}
	if p.Offset < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'offset ne peut pas être négatif.", nil)
	}
	return nil
}

type GetNotificationsOutput struct {
	Notifications []NotificationView `json:"notifications"` // ✅ Remplacé par NotificationView
}
