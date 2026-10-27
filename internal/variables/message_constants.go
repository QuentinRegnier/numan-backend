package variables

// ############################################################################
// # PARAMÈTRES ET CONSTANTES DU DOMAINE MESSAGES ET NOTIFICATIONS
// ############################################################################

const (
	// Types de messages (Énumérations BDD immuables)
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

	// System Actions (Clés protocolaires immuables)
	SysActionMemberJoined         = "member_joined"
	SysActionMemberJoinedWithLink = "member_joined_link"
	SysActionMemberJoinedThanksTo = "memner_joined_thanks_to"
	SysActionMemberBanned         = "member_banned"
	SysActionMemberPromoted       = "member_promoted"
	SysActionMemberDemoted        = "member_demoted"
	SysActionMemberMuted          = "member_muted"
)

var (
	// Liste des types de widgets autorisés (Modifiable à chaud)
	AllowedWidgetTypes = []string{"action_button", "link_card", "poll"}
)

// ShouldAllowNewReactions determines whether a message type is permitted to allow new reactions.
// Restricted message types 6 (Invite), 8 (System), 9 (Survey), and 10 (Widget) strictly prohibit new reactions.
func ShouldAllowNewReactions(messageType int) bool {
	switch messageType {
	case MessageTypeInvite, MessageTypeSystem, MessageTypeSurvey, MessageTypeWidget:
		return false
	default:
		return true
	}
}
