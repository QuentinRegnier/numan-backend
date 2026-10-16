package variables

// ============================================================================
// PARAMÈTRES ET CONSTANTES DU DOMAINE CONVERSATIONS & MESSAGERIE
// ============================================================================

const (
	// Types de conversation (Énumérations BDD immuables)
	ConversationTypeDirect        = 0 // Message Privé (MP)
	ConversationTypeGroup         = 1 // Groupe standard
	ConversationTypeCommunityPriv = 2 // Communauté privée
	ConversationTypeCommunityPub  = 3 // Communauté publique

	ConversationStateAll           = 0
	ConversationStatePrivateDelete = -1
	ConversationStateGroupDelete   = -2

	// États globaux d'une conversation (Énumérations BDD immuables)
	ConversationStateActive   = 0
	ConversationStateArchived = 1
)

var (
	// Seuils et plafonds modifiables à chaud
	MaxPinConversation                = 3
	MaxActiveParticipantsForBroadcast = 50
	MaxZsetInbox                      = 100
	CommunityMinCreationGrade         = 2
	MaxPublicCommunitiesColl          = 1
)
