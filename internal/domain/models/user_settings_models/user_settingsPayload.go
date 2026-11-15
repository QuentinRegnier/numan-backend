package user_settings_models

import (
	"math"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

type PrivacySettings struct {
	ProfileVisibility      int  `json:"profile_visibility" bson:"profile_visibility"`           // 0: Public, 1: Abonnés, 2: Amis
	PostVisibilityDefault  int  `json:"post_visibility_default" bson:"post_visibility_default"` // 0: Public, 1: Abonnés, 2: Amis (Mode 3 Privé supprimé)
	ConversationPermission int  `json:"conversation_permission" bson:"conversation_permission"` // 0: Tout le monde, 1: Abonnés, 2: Amis, 3: Personne
	AddGroupPermission     int  `json:"add_group_permission" bson:"add_group_permission"`       // 0: Tout le monde, 1: Amis, 2: Personne
	AllowTagging           int  `json:"allow_tagging" bson:"allow_tagging"`                     // 0: Tout le monde, 1: Abonnés, 2: Amis, 3: Personne
	AllowMentions          int  `json:"allow_mentions" bson:"allow_mentions"`                   // 0: Tout le monde, 1: Abonnés, 2: Amis, 3: Personne
	ShowOnlineStatus       bool `json:"show_online_status" bson:"show_online_status"`
	SendReadReceipts       bool `json:"send_read_receipts" bson:"send_read_receipts"` // NOUVEAU
	SearchByEmailPhone     bool `json:"search_by_email_phone" bson:"search_by_email_phone"`
	ShowLocation           bool `json:"show_location" bson:"show_location"`
	HideConnections        int  `json:"hide_connections" bson:"hide_connections"` // 0: Public, 1: Abonnés, 2: Amis, 3: Privé
}

// Validate vérifie que tous les paramètres énumérés de confidentialité respectent le domaine autorisé.
func (p *PrivacySettings) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres de confidentialité sont nuls.", nil)
	}
	if p.ProfileVisibility < variables.ProfileVisibilityPublic || p.ProfileVisibility > variables.ProfileVisibilityFriends {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La visibilité du profil est invalide.", nil)
	}
	if p.PostVisibilityDefault < variables.PostVisibilityAll || p.PostVisibilityDefault > variables.PostVisibilityFriend {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La visibilité par défaut des publications est invalide (0: Public, 1: Abonnés, 2: Amis).", nil)
	}
	if p.ConversationPermission < 0 || p.ConversationPermission > 3 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La permission de conversation est invalide.", nil)
	}
	if p.AddGroupPermission < 0 || p.AddGroupPermission > 2 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La permission d'ajout aux groupes est invalide.", nil)
	}
	if p.AllowTagging < variables.TagPermissionEveryone || p.AllowTagging > variables.TagPermissionNobody {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La permission de marquage (tagging) est invalide.", nil)
	}
	if p.AllowMentions < variables.TagPermissionEveryone || p.AllowMentions > variables.TagPermissionNobody {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La permission de mention est invalide.", nil)
	}
	if p.HideConnections < variables.ConnectionsVisibilityPublic || p.HideConnections > variables.ConnectionsVisibilityPrivate {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La visibilité des relations est invalide.", nil)
	}
	return nil
}

type NotificationSettings struct {
	MasterPushEnabled   bool `json:"master_push_enabled" bson:"master_push_enabled"`
	MasterEmailEnabled  bool `json:"master_email_enabled" bson:"master_email_enabled"`
	NotifyLikes         bool `json:"notify_likes" bson:"notify_likes"`
	NotifyComments      bool `json:"notify_comments" bson:"notify_comments"`
	NotifyMentions      bool `json:"notify_mentions" bson:"notify_mentions"`
	NotifyNewFollower   bool `json:"notify_new_follower" bson:"notify_new_follower"`
	NotifyFriendRequest bool `json:"notify_friend_request" bson:"notify_friend_request"`
	NotifyMessages      bool `json:"notify_messages" bson:"notify_messages"`
	NotifyGroupInvites  bool `json:"notify_group_invites" bson:"notify_group_invites"` // NOUVEAU
}

// Validate vérifie que la structure des préférences de notification est non nulle.
func (p *NotificationSettings) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres de notification sont nuls.", nil)
	}
	return nil
}

// NOUVELLE STRUCTURE
type DisplayAndContentSettings struct {
	Theme          int  `json:"theme" bson:"theme"`                       // 0: Système, 1: Clair, 2: Sombre
	Language       int  `json:"language" bson:"language"`                 // Code langue
	SafeForCommute bool `json:"safe_for_commute" bson:"safe_for_commute"` // NOUVEAU : Floutage par défaut
}

// Validate vérifie que les préférences d'affichage et de contenu sont dans les bornes valides.
func (p *DisplayAndContentSettings) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres d'affichage sont nuls.", nil)
	}
	if p.Theme < 0 || p.Theme > 2 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le thème sélectionné est invalide (0: Système, 1: Clair, 2: Sombre).", nil)
	}
	if p.Language < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le code langue ne peut pas être négatif.", nil)
	}
	return nil
}

type UserSettingsPayload struct {
	ID                 int64                     `json:"id" bson:"id"`
	UserID             int64                     `json:"user_id" bson:"user_id"`
	Privacy            PrivacySettings           `json:"privacy" bson:"privacy"`
	Notifications      NotificationSettings      `json:"notifications" bson:"notifications"`
	DisplayAndContent  DisplayAndContentSettings `json:"display_and_content" bson:"display_and_content"` // REMPLACE Theme et Language
	TelemetryVector    []float32                 `json:"telemetry_vector" bson:"telemetry_vector"`
	TelemetryTags      []string                  `json:"telemetry_tags" bson:"telemetry_tags"`
	TelemetryTimestamp int64                     `json:"telemetry_timestamp" bson:"telemetry_timestamp"`
	CreatedAt          int64                     `json:"created_at" bson:"created_at"`
	UpdatedAt          int64                     `json:"updated_at" bson:"updated_at"`
}

// Validate vérifie les invariants métier complets de UserSettingsPayload avant mise en cache ou persistance.
func (p *UserSettingsPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload des paramètres utilisateur est nul.", nil)
	}
	if p.ID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant des paramètres utilisateur doit être strictement positif.", nil)
	}
	if p.UserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur doit être strictement positif.", nil)
	}
	if err := p.Privacy.Validate(); err != nil {
		return err
	}
	if err := p.Notifications.Validate(); err != nil {
		return err
	}
	if err := p.DisplayAndContent.Validate(); err != nil {
		return err
	}
	if len(p.TelemetryVector) > 0 {
		if len(p.TelemetryVector) != variables.VectorDimTotal {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La dimension du vecteur télémétrique est invalide.", nil)
		}
		for _, v := range p.TelemetryVector {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le vecteur télémétrique contient une valeur non finie.", nil)
			}
		}
	}
	if p.TelemetryTimestamp < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'horodatage télémétrique ne peut pas être négatif.", nil)
	}
	return nil
}
