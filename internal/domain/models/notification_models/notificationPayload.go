package notification_models

import (
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// NotificationPayload représente un événement polymorphe dans le flux d'activité global.
type NotificationPayload struct {
	ID        int64  `json:"id" bson:"id"`
	UserID    int64  `json:"user_id" bson:"user_id"`     // Le propriétaire de la boîte aux lettres
	ActorID   int64  `json:"actor_id" bson:"actor_id"`   // Celui qui a déclenché l'événement
	Type      string `json:"type" bson:"type"`           // Polymorphe: "new_message", "post_like", "new_follower", etc.
	TargetID  int64  `json:"target_id" bson:"target_id"` // L'ID de l'entité concernée (Post, Conversation, etc.)
	IsRead    bool   `json:"is_read" bson:"is_read"`     // État de lecture (pastille)
	CreatedAt int64  `json:"created_at" bson:"created_at"`
}

// Validate vérifie les invariants métier d'une notification avant mise en cache ou persistance.
func (p *NotificationPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de notification est nul.", nil)
	}
	if p.UserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du destinataire doit être strictement positif.", nil)
	}
	if p.ActorID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de l'acteur doit être strictement positif.", nil)
	}
	if p.UserID == p.ActorID {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Une auto-notification n'est pas autorisée.", nil)
	}
	if strings.TrimSpace(p.Type) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le type de notification est obligatoire.", nil)
	}
	if p.TargetID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant cible ne peut pas être négatif.", nil)
	}
	return nil
}
