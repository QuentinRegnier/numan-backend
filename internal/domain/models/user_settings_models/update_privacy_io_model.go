package user_settings_models

type UpdatePrivacyInput struct {
	ProfileVisibility      int  `json:"profile_visibility" binding:"omitempty,oneof=0 1"`
	PostVisibilityDefault  int  `json:"post_visibility_default" binding:"omitempty,oneof=0 1 2"`   // 0: Public, 1: Abonnés, 2: Amis (Mode 3 Privé rejeté)
	ConversationPermission int  `json:"conversation_permission" binding:"omitempty,oneof=0 1 2 3"` // 3 = Personne
	AddGroupPermission     int  `json:"add_group_permission" binding:"omitempty,oneof=0 1 2"`      // 0=All, 1=Amis, 2=Personne
	AllowTagging           int  `json:"allow_tagging" binding:"omitempty,oneof=0 1 2"`
	AllowMentions          int  `json:"allow_mentions" binding:"omitempty,oneof=0 1 2"`
	ShowOnlineStatus       bool `json:"show_online_status" binding:"omitempty"`
	SendReadReceipts       bool `json:"send_read_receipts" binding:"omitempty"` // NOUVEAU
	SearchByEmailPhone     bool `json:"search_by_email_phone" binding:"omitempty"`
	ShowLocation           bool `json:"show_location" binding:"omitempty"`
	HideConnections        int  `json:"hide_connections" binding:"omitempty"` // NOUVEAU
}

type UpdatePrivacyOutput struct {
	UserSettingsUpdateAt int64 `json:"user_settings_update_at"`
}
