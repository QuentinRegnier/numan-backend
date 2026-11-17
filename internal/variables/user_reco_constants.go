package variables

import "time"

// ============================================================================
// INVARIANTS ET CLÉS STRUCTURELLES — USER RECOMMENDATION SYSTEM (IMMUABLES)
// ============================================================================

const (
	// Redis Sorted Sets (L1 Speed Cache)
	RedisKeyUserRecoListGeo    = "user:reco:list:geo"
	RedisKeyUserRecoListAff    = "user:reco:list:aff"
	RedisKeyUserRecoListTaste  = "user:reco:list:taste"
	RedisKeyUserRecoListGlobal = "user:reco:list:global"

	// Redis Language Set Index
	RedisKeyUsersLang = "users:lang"

	// Redis Cuckoo Filter for Skip Protection
	RedisKeyCuckooUserRecoSkips = "cuckoo:user_reco:skips"

	// Redis Compute Lock
	RedisKeyLockUserRecoCompute = "lock:user_reco:compute"

	// MongoDB Collection Name (L2 Warm Storage)
	MongoCollUserRecoSkips = "user_reco_skips"

	// Cuckoo Filter Invariants (N = 3000, ε = 0.01, ~6.3 KB per user)
	UserRecoCuckooCapacity        = 3000
	UserRecoCuckooFalsePositive   = 0.01
	UserRecoCuckooBucketSize      = 4
	UserRecoCuckooMaxIterations   = 500
	UserRecoCuckooExpansionFactor = 1

	// Skip TTL in MongoDB (604800 seconds = 7 days)
	UserRecoSkipTTLSeconds = 604800

	// Pagination & Sorting Constants for GET /users/recommendations
	UserRecoSortGlobal = "global"
	UserRecoSortGeo    = "geo"
	UserRecoSortAff    = "aff"
	UserRecoSortTaste  = "taste"

	UserRecoDefaultLimit int64 = 20
	UserRecoMinLimit     int64 = 1
	UserRecoMaxLimit     int64 = 50
)

// ============================================================================
// PARAMÈTRES DYNAMIQUES — USER RECOMMENDATION ENGINE (HOT-RELOADABLE)
// ============================================================================

var (
	// Poids ω des amis en commun dans ScoreAff = (1.0 + MutualFriendsCount * ω) * CommunityMultiplier
	UserRecoMutualFriendWeight = 0.35

	// Échelle de décroissance spatiale (en km) pour ScoreGeo = exp(-distanceKm / scaleKm) * CommunityMultiplier
	UserRecoGeoDecayScaleKm = 50.0

	// Nombre maximum de candidats retenus en Phase 1 (Candidate Pre-Filtering : ~1,000–5,000)
	UserRecoMaxCandidates = 5000

	// Nombre maximum d'utilisateurs conservés par liste de recommandation Redis ZSET (Top 150)
	UserRecoTopK = 150

	// Durée du verrou d'exécution d'un recalcul forcé ou planifié (~12 heures)
	UserRecoComputeLockTTL = 12 * time.Hour

	// Durée de vie des listes de recommandation pré-calculées en L1 Redis
	UserRecoListTTL = 48 * time.Hour

	// Attente maximale bornée lors d'un appel synchrone en cas de cache miss initial
	UserRecoSyncWaitTimeout = 150 * time.Millisecond

	// Intervalle d'exécution du Cron de rafraîchissement automatique (24–48 heures)
	UserRecoCronInterval = 24 * time.Hour

	// Fenêtre d'activité (en jours) pour considérer un utilisateur actif lors du Cron
	UserRecoActiveWindowDays = 30

	// Taille de lot (batch) pour la pagination des utilisateurs actifs dans le Cron
	UserRecoCronBatchSize = 200
)
