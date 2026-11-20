package variables

// ============================================================================
// ÉTATS DE VISIBILITÉ DES ARTICLES (ÉNUMÉRATIONS BDD IMMUABLES)
// ============================================================================

const (
	ArticleVisibilitySoftDeleted = -1 // Article supprimé logiquement (masqué de tous)
	ArticleVisibilityPublished   = 0  // Article publié (visible tant que non expiré)
	ArticleVisibilityDraft       = 1  // Brouillon (visible uniquement par les administrateurs)
)

// ============================================================================
// PRÉFIXES DE STOCKAGE OBJET ET BUCKETS
// ============================================================================

const (
	ArticleMediaStoragePrefix = "articles/"
	MapsStoragePrefix         = "maps/"
	StaticStoragePrefix       = "static/"

	DefaultPrivateBucketName = "numan-bucket"
	DefaultPublicBucketName  = "numan-public"

	MongoCollArticles = "content.articles"
)

// ============================================================================
// TIERS DE QUALITÉ ET TYPES D'ASSETS D'ARTICLES
// ============================================================================

const (
	ArticleQualityTierStandard = "standard"
	ArticleQualityTierHigh     = "high"

	ArticleImageMaxDimStandard = 1024
	ArticleImageMaxDimHigh     = 2048

	ArticleAssetKindImage    = "image"
	ArticleAssetKindMarkdown = "markdown"
)

// ============================================================================
// CLÉS REDIS L1 PERSISTANTES (TTL = 0 / NON-ÉVICTABLES SOUS VOLATILE-LFU)
// ============================================================================

const (
	RedisKeyArticlesIndexData  = "articles:index:data"
	RedisKeyArticlesIndexOrder = "articles:index:order"
	RedisKeyArticlesIndexInit  = "articles:index:initialized"
	RedisKeyArticlesRebuildLck = "articles:index:rebuild_lock"
)

// ============================================================================
// LIMITES ET QUOTAS DU DOMAINE ARTICLES
// ============================================================================

var (
	// Limites sur les champs métier d'un article
	MaxArticleTitleLength      = 200
	MaxArticleSummaryLength    = 500
	MaxArticleAttachments      = 50
	MaxArticlePartnerURLLength = 512

	// Pagination de la liste des articles
	DefaultArticleListLimit = 20
	MaxArticleListLimit     = 100

	// Limites de taille d'upload des assets d'articles (en octets)
	MaxArticleStaticImageBytes  = int64(15 * 1024 * 1024) // 15 Mo
	MaxArticleAnimatedGIFBytes  = int64(5 * 1024 * 1024)  // 5 Mo
	MaxArticleAnimatedWebPBytes = int64(5 * 1024 * 1024)  // 5 Mo
	MaxArticleMarkdownBytes     = int64(256 * 1024)       // 256 Ko

	// Limite de pixels maximale pour éviter les bombes de décompression sur images d'articles
	ArticleMaxImagePixels = 8192 * 8192
)
