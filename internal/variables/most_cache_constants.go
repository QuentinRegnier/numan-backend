package variables

// ############################################################################
// # CLÉS REDIS — NOMENCLATURE UNIFIÉE (IMMUABLES)
// ############################################################################

const (
	RedisKeyTrendGlobalHourly  = "most_cache:trend:global:hourly:%s"
	RedisKeyTrendGlobalDaily   = "most_cache:trend:global:daily:%s"
	RedisKeyTrendTagDaily      = "most_cache:trend:tag:%s:daily:%s"
	RedisKeyTrendTagWeekly     = "most_cache:trend:tag:%s:weekly:%s"
	RedisKeyHashtagLeaderboard = "most_cache:trend:hashtag:leaderboard"

	// Classements Stricts (Pour l'interface utilisateur uniquement)
	RedisKeyStrictLikes  = "most_cache:strict:likes"
	RedisKeyStrictViews  = "most_cache:strict:views"
	RedisKeyStrictRecent = "most_cache:strict:recent"
)

// ############################################################################
// # LIMITES OPÉRATIONNELLES ET PARAMÈTRES MATHÉMATIQUES (DYNAMIQUES)
// ############################################################################

var (
	MaxStrictElements    = 5000 // Top absolu pour l'UI (Likes, Vues)
	MaxTagElements       = 5000 // Taille max pour l'historique brut d'un tag
	MaxTrendingTagsInRAM = 1000 // Top 1000 des tags mondiaux (UI)
	MaxPostsPerTagInRAM  = 1000 // Limite stricte pour le Speed Cache par tag

	TDDMaxZSET    = 500  // MAX_ZSET — taille max par ZSET de tendance
	TDDCandidates = 1000 // |C| — taille de l'ensemble de candidats pour le feed_service
	TDDFeedSize   = 50   // K_feed — taille du feed_service personnalisé retourné

	TDDSigmaHours         = 2.0  // σ_h (h) — lissage gaussien sur l'heure
	TDDWeightView         = 0.1  // w_view — Poids d'une vue pure
	TDDPhiReported        = 0.5  // φ_mod — pénalité si le post_service est signalé
	TDDHashtagWindowHours = 48.0 // Fenêtre de validité pour les trends hashtags
)
