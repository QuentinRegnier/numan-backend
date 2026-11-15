package lite_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

type MemberSettingsLite struct {
	IsMuted           int   `bson:"is_muted" json:"is_muted" msgpack:"is_muted"`
	MuteExpireAt      int64 `bson:"mute_expire_at" json:"mute_expire_at" msgpack:"mute_expire_at"`
	Pinned            int   `bson:"pinned" json:"pinned" msgpack:"pinned"`
	MediaAutoDownload bool  `bson:"media_auto_download" json:"media_auto_download" msgpack:"media_auto_download"`
	RestrictedUntil   int64 `json:"restricted_until" bson:"restricted_until" msgpack:"restricted_until"`
}

// Validate vérifie les invariants métier des paramètres allégés d'un membre.
func (p *MemberSettingsLite) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres allégés du membre sont nuls.", nil)
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

type MemberLiteRequest struct {
	ConversationID    int64              `bson:"conversation_id" json:"conversation_id" msgpack:"conversation_id"`
	UserID            int64              `bson:"user_id" json:"user_id" msgpack:"user_id"`
	Role              int                `bson:"role" json:"role" msgpack:"role"`
	Settings          MemberSettingsLite `bson:"settings" json:"settings" msgpack:"settings"`
	UnreadCount       int                `bson:"unread_count" json:"unread_count" msgpack:"unread_count"`
	FrozenMessageID   int64              `bson:"frozen_message_id" json:"frozen_message_id" msgpack:"frozen_message_id"`
	LastReadMessageID int64              `bson:"last_read_message_id" json:"last_read_message_id" msgpack:"last_read_message_id"` // ✅ NOUVEAU
	JoinedAt          int64              `bson:"joined_at" json:"joined_at" msgpack:"joined_at"`
}

// Validate vérifie les invariants métier d'un membre allégé en cache.
func (p *MemberLiteRequest) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload allégé du membre est nul.", nil)
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
	if p.JoinedAt < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'horodatage d'adhésion ne peut pas être négatif.", nil)
	}
	return p.Settings.Validate()
}
