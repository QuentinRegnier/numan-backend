package variables

// ############################################################################
// # PARAMÈTRES ET CONSTANTES DU DOMAINE MESSAGES ET NOTIFICATIONS
// ############################################################################

const (
	// Types de messages
	MessageTypeText   = 0
	MessageTypeVoice  = 1
	MessageTypeMedia  = 2
	MessageTypeGIF    = 3
	MessageTypeVideo  = 4
	MessageTypePost   = 5
	MessageTypeInvite = 6
	MessageTypeLink   = 7
	MessageTypeSystem = 8
	MessageTypeSurvey = 9
	MessageTypeWidget = 10

	// System Actions
	SysActionMemberJoinedWithLink = "member_joined_link"
	SysActionMemberJoinedThanksTo = "memner_joined_thanks_to"
	SysActionMemberBanned         = "member_banned"
	SysActionMemberPromoted       = "member_promoted"
	SysActionMemberDemoted        = "member_demoted"
	SysActionMemberMuted          = "member_muted"
)

var AllowedWidgetTypes = []string{"action_button", "link_card", "poll"}
