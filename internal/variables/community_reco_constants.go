package variables

import "time"

// ============================================================================
// INVARIANTS ET CLÉS STRUCTURELLES — PUBLIC COMMUNITY RECOMMENDATION SYSTEM
// ============================================================================

const (
	// Redis L1 Collections (Public Community Recommendation System)
	RedisKeyCommunityRecoRanked   = "community:reco:ranked"
	RedisKeyCommunityRecoPins     = "community:reco:pins"
	RedisKeyCommunityRecoMsgCount = "community:reco:msg_count"
	RedisKeyCommunityRecoAuthors  = "community:reco:authors"
	RedisKeyCommunityRecoGrowth   = "community:reco:growth"
	RedisKeyUserPublicComms       = "user:public_comms"

	// Cron distributed lock
	CommunityRecoCronLockKey = "community_reco_cron"
	CommunityRecoCronLockTTL = 10 * time.Minute

	// Telemetry retention (8 days to safely cover the 7-day UTC rolling window)
	CommunityRecoTelemetryTTL = 8 * 24 * time.Hour

	// API bounds
	CommunityRecoDefaultLimit = 20
	CommunityRecoMaxLimit     = 50
	CommunityRecoMinOffset    = 0
	CommunityRecoMaxOffset    = 500
	CommunityRecoMinPinSlot   = 1
	CommunityRecoMaxPinSlot   = 100

	// Upper safety bounds for dynamic variable validation
	CommunityRecoMaxFriendEvalCap      = 5000
	CommunityRecoMaxMemberSampleCap    = 5000
	CommunityRecoMinCronInterval       = 1 * time.Minute
	CommunityRecoMaxCronInterval       = 168 * time.Hour
	CommunityRecoMaxMultiplier         = 10.0
	CommunityRecoMaxFreshnessDays      = 365
	CommunityRecoMaxPriorStrength      = 10000.0
	CommunityRecoMembershipSeedBatchSz = 10000
)

// ============================================================================
// PARAMÈTRES DYNAMIQUES — PUBLIC COMMUNITY RECOMMENDATION SYSTEM (18 VARS)
// Hot-reloadable via PATCH /admin/variables/update
// ============================================================================

var (
	// Poids du score global (utilisés au prochain cycle cron)
	CommunityRecoWeightMembers  = 0.25
	CommunityRecoWeightDynamism = 0.50
	CommunityRecoWeightGrowth   = 0.25

	// Confiance hiérarchique (utilisés au prochain cycle cron)
	CommunityRecoTrustAlphaOwner      = 0.45
	CommunityRecoTrustAlphaAdmin      = 0.30
	CommunityRecoTrustAlphaMember     = 0.25
	CommunityRecoTrustPriorMean       = 0.50
	CommunityRecoTrustPriorStrength   = 10.0
	CommunityRecoTrustMemberSampleCap = 200

	// Multiplicateurs de mode d'interaction (utilisés au prochain cycle cron)
	CommunityRecoModeMultFreeChat     = 1.15
	CommunityRecoModeMultSubreddit    = 1.15
	CommunityRecoModeMultAnnouncement = 0.45

	// Multiplicateur partenaire (utilisé au prochain cycle cron)
	CommunityRecoPartnerMultiplier = 1.25

	// Fraîcheur linéaire : 1.0 + CommunityRecoFreshnessBoost à la création (1.30),
	// décroissance linéaire sur CommunityRecoFreshnessDays (14j) vers 1.00.
	// Utilisés au prochain cycle cron et à l'insertion initiale lors de la création.
	CommunityRecoFreshnessBoost = 0.30
	CommunityRecoFreshnessDays  = 14

	// Affinité amis 1er degré (utilisés immédiatement à la requête suivante)
	CommunityRecoFriendBoostGamma = 0.25
	CommunityRecoFriendEvalCap    = 500

	// Cadence du worker cron (utilisée au prochain redémarrage/cycle du ticker)
	CommunityRecoCronInterval = 2 * time.Hour
)
