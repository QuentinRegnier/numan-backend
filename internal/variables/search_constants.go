package variables

// ############################################################################
// # CONSTANTES PROTOCOLAIRES & MODES SQL DU DOMAINE RECHERCHE (IMMUABLES)
// ############################################################################

const (
	// Filtres de recherche textuelle pour les requêtes frontend
	SearchFilterViews    = "views"
	SearchFilterLikes    = "likes"
	SearchFilterComments = "comments"
	SearchFilterRecent   = "recent"
	SearchFilterOldest   = "oldest"
	SearchFilterTrend    = "trend"

	// Correspondances pour le tri (Order Mode) dans PostgreSQL
	OrderModeViews    = 0
	OrderModeLikes    = 1
	OrderModeComments = 2
	OrderModeRecent   = 3
	OrderModeOldest   = 4
)

// ############################################################################
// # LIMITES DYNAMIQUES DE RECHERCHE
// ############################################################################

var (
	DefaultAutocompleteLimit int64 = 15
	DefaultGlobalSearchLimit int64 = 10
)
