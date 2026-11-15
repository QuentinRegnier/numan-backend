package user_settings_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// UpdateNotificationsInput (Remplacement intégral PUT)
type UpdateNotificationsInput struct {
	MasterPushEnabled   bool `json:"master_push_enabled"`
	MasterEmailEnabled  bool `json:"master_email_enabled"`
	NotifyLikes         bool `json:"notify_likes"`
	NotifyComments      bool `json:"notify_comments"`
	NotifyMentions      bool `json:"notify_mentions"`
	NotifyNewFollower   bool `json:"notify_new_follower"`
	NotifyFriendRequest bool `json:"notify_friend_request"`
	NotifyMessages      bool `json:"notify_messages"`
	NotifyGroupInvites  bool `json:"notify_group_invites"` // NOUVEAU
}

// Validate vérifie que le payload des préférences de notification est non nul.
func (p *UpdateNotificationsInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	return nil
}

type UpdateNotificationsOutput struct {
	UserSettingsUpdateAt int64 `json:"user_settings_update_at"`
}
