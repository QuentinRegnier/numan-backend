package models

// CommunityMode représente le mode d'interaction d'une communauté publique.
type CommunityMode string

const (
	// CommunityModeFreeChat : tout le monde peut écrire librement (WritePermission == 0).
	CommunityModeFreeChat CommunityMode = "free_chat"
	// CommunityModeSubreddit : les admins postent en racine, les membres répondent dans les threads (WritePermission == 2).
	CommunityModeSubreddit CommunityMode = "subreddit"
	// CommunityModeAnnouncement : seuls les admins/owners peuvent publier (WritePermission == 1).
	CommunityModeAnnouncement CommunityMode = "announcement"
)

// ResolveCommunityModeFromWritePermission est la source de vérité unique qui dérive
// le CommunityMode à partir du paramètre WritePermission de ConversationSettings.
//
// Mapping vérifié sur create_message_service.go (L144-153) et conversationPayload.go (L13, L89-96) :
//   - 0 -> free_chat    (tous les membres peuvent écrire)
//   - 1 -> announcement (seuls les admins/owners peuvent écrire)
//   - 2 -> subreddit    (admins postent, membres répondent en threads)
//   - toute autre valeur inattendue -> subreddit (mode par défaut sécurisé des communautés publiques)
func ResolveCommunityModeFromWritePermission(writePermission int) CommunityMode {
	switch writePermission {
	case 0:
		return CommunityModeFreeChat
	case 1:
		return CommunityModeAnnouncement
	case 2:
		return CommunityModeSubreddit
	default:
		return CommunityModeSubreddit
	}
}

// IsValid retourne true si le mode fait partie des 3 modes d'interaction supportés.
func (m CommunityMode) IsValid() bool {
	switch m {
	case CommunityModeFreeChat, CommunityModeSubreddit, CommunityModeAnnouncement:
		return true
	default:
		return false
	}
}
