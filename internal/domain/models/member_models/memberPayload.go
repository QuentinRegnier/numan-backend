package member_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

type MemberSettings struct {
	IsMuted           int   `json:"is_muted" bson:"is_muted" msgpack:"is_muted"`
	MuteExpireAt      int64 `json:"mute_expire_at" bson:"mute_expire_at" msgpack:"mute_expire_at"`
	Pinned            int   `json:"pinned" bson:"pinned" msgpack:"pinned"`
	MediaAutoDownload bool  `json:"media_auto_download" bson:"media_auto_download"`
	RestrictedUntil   int64 `json:"restricted_until" bson:"restricted_until" msgpack:"restricted_until"`
}

// Validate vérifie la validité des paramètres personnels d'un membre.
func (p *MemberSettings) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres du membre sont nuls.", nil)
	}
	if p.IsMuted < 0 || p.IsMuted > 2 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le mode silencieux doit être compris entre 0 et 2.", nil)
	}
	if p.MuteExpireAt < -1 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La date d'expiration du mode silencieux est invalide.", nil)
	}
	if p.Pinned < -1 || p.Pinned >= variables.MaxPinConversation {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'index d'épinglage est invalide.", nil)
	}
	if p.RestrictedUntil < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La durée de restriction ne peut pas être négative.", nil)
	}
	return nil
}

type MemberPayload struct {
	ID                int64          `json:"id" bson:"id"`
	ConversationID    int64          `json:"conversation_id" bson:"conversation_id"`
	UserID            int64          `json:"user_id" bson:"user_id"`
	Role              int            `json:"role" bson:"role"`
	Settings          MemberSettings `json:"settings" bson:"settings"`
	JoinedAt          int64          `json:"joined_at" bson:"joined_at"`
	UnreadCount       int            `json:"unread_count" bson:"unread_count"`
	FrozenMessageID   int64          `json:"frozen_message_id" bson:"frozen_message_id"`
	LastReadMessageID int64          `json:"last_read_message_id" bson:"last_read_message_id"` // ✅ NOUVEAU
	CreatedAt         int64          `json:"created_at" bson:"created_at"`
	UpdatedAt         int64          `json:"updated_at" bson:"updated_at"`
}

// Validate vérifie l'intégrité complète d'un MemberPayload avant mise en cache ou persistance.
func (p *MemberPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload du membre est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation du membre doit être strictement positif.", nil)
	}
	if p.UserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur du membre doit être strictement positif.", nil)
	}
	if p.Role < variables.MemberRolePendingApproval || p.Role > variables.MemberRoleOwner {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le rôle du membre est invalide.", nil)
	}
	if p.UnreadCount < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le compteur de messages non lus ne peut pas être négatif.", nil)
	}
	if p.FrozenMessageID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de message figé ne peut pas être négatif.", nil)
	}
	if p.LastReadMessageID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du dernier message lu ne peut pas être négatif.", nil)
	}
	if err := p.Settings.Validate(); err != nil {
		return err
	}
	return nil
}

// DefaultMemberSettings génère les paramètres par défaut basés sur le type de la conversation.
func DefaultMemberSettings(convType int) MemberSettings {
	switch convType {
	case 3:
		return MemberSettings{
			IsMuted:           2,
			MuteExpireAt:      -1,
			Pinned:            -1,
			MediaAutoDownload: true,
			RestrictedUntil:   0,
		}
	default:
		return MemberSettings{
			IsMuted:           0,
			MuteExpireAt:      0,
			Pinned:            -1,
			MediaAutoDownload: true,
			RestrictedUntil:   0,
		}
	}
}
