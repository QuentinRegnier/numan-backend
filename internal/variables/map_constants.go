package variables

// ############################################################################
// # CONSTANTES STRUCTURELLES ET ÉNUMÉRATIONS DU DOMAINE DISCOVERY MAP (POIs)
// ############################################################################

const (
	// Limites de validation des champs d'un Point d'Intérêt (POI)
	MaxPOITitleLength       = 200
	MaxPOIDescriptionLength = 500
	MaxPOIAddressLength     = 500
	MaxPOIPhoneLength       = 64
	MaxPOIWebsiteLength     = 512

	// Bornes géographiques WGS84
	MinLatitude  = -90.0
	MaxLatitude  = 90.0
	MinLongitude = -180.0
	MaxLongitude = 180.0

	// Bornes de notation
	MinPOIRating       = 0.0
	MaxPOIRating       = 5.0
	MinPOIReviewRating = 1
	MaxPOIReviewRating = 5

	// Limites des avis / notes POI
	MaxPOIReviewContentLength = 2000
	DefaultPOIReviewLimit     = 20
	MaxPOIReviewLimit         = 100

	// Statuts de visibilité d'un POI et de ses avis (Soft Delete / Public)
	POIVisibilitySoftDeleted       = -1
	POIVisibilityPublic            = 0
	POIReviewVisibilitySoftDeleted = -1
	POIReviewVisibilityPublic      = 0
)

// ############################################################################
// # QUOTAS DYNAMIQUES & BATCHES DU DOMAINE MAP
// ############################################################################

var (
	MaxMediaPerPOI       = 10
	MaxPOIGeoSearchCount = 500
	MaxPOIImportBatch    = 1000
	MaxPOISetBatch       = 100
	MaxPOIUpdateBatch    = 100
	MaxPOIDeleteBatch    = 500
	MaxZsetPOIReview     = 200
)
