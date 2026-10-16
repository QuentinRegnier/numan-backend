package user_settings_models

type PrivacySettings struct {
	ProfileVisibility      int  `json:"profile_visibility" bson:"profile_visibility"`           // 0: Public, 2: Amis
	PostVisibilityDefault  int  `json:"post_visibility_default" bson:"post_visibility_default"` // 0: Public, 1: Abonnés, 2: Amis (Mode 3 Privé supprimé)
	ConversationPermission int  `json:"conversation_permission" bson:"conversation_permission"` // 0: Tout le monde, 1: Abonnés, 2: Amis, 3: Personne
	AddGroupPermission     int  `json:"add_group_permission" bson:"add_group_permission"`       // 0: Tout le monde, 1: Amis, 2: Personne
	AllowTagging           int  `json:"allow_tagging" bson:"allow_tagging"`                     // 0: Tout le monde, 1: Abonnés, 2: Amis
	AllowMentions          int  `json:"allow_mentions" bson:"allow_mentions"`                   // 0: Tout le monde, 1: Abonnés, 2: Amis
	ShowOnlineStatus       bool `json:"show_online_status" bson:"show_online_status"`
	SendReadReceipts       bool `json:"send_read_receipts" bson:"send_read_receipts"` // NOUVEAU
	SearchByEmailPhone     bool `json:"search_by_email_phone" bson:"search_by_email_phone"`
	ShowLocation           bool `json:"show_location" bson:"show_location"`
	HideConnections        int  `json:"hide_connections" bson:"hide_connections"` // NOUVEAU
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

// NOUVELLE STRUCTURE
type DisplayAndContentSettings struct {
	Theme          int  `json:"theme" bson:"theme"`                       // 0: Système, 1: Clair, 2: Sombre
	Language       int  `json:"language" bson:"language"`                 // Code langue
	SafeForCommute bool `json:"safe_for_commute" bson:"safe_for_commute"` // NOUVEAU : Floutage par défaut
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
