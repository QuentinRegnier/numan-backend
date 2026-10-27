package variables

// ############################################################################
// # CONSTANTES STRUCTURELLES ET ÉNUMÉRATIONS DU DOMAINE POSTS (IMMUABLES)
// ############################################################################

const (
	// Préfixe utilisé pour tagger automatiquement l'auteur dans ses propres publications
	AuthorTagPrefix = "user_"

	// Niveaux de permissions pour les identifications (tags) et mentions
	TagPermissionEveryone  = 0
	TagPermissionFollowers = 1
	TagPermissionFriends   = 2
	TagPermissionNobody    = 3

	// Niveaux de visibilité pour les posts (-2=Modéré, -1=Supprimé, 0=Public, 1=Abonnés, 2=Amis, 3=Quarantaine/Shadowban)
	PostVisibilityModerated   = -2
	PostVisibilityDeleted     = -1
	PostVisibilitySoftDeleted = -1
	PostVisibilityAll         = 0
	PostVisibilitySubcriber   = 1
	PostVisibilityFriend      = 2
	PostVisibilityQuarantine  = 3
)

// ############################################################################
// # QUOTAS DYNAMIQUES PAR PUBLICATION
// ############################################################################

var (
	MaxHashtagsPerPost    = 20
	MaxIdentifiersPerPost = 10
	MaxMediaPerPost       = 4
)
