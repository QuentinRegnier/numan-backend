package redis

import (
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ============================================================================
// 1. VARIABLES GLOBALES (100 COLLECTIONS ORGANISÉES EN 12 FAMILLES)
// ============================================================================
//
// L'ordre des familles ci-dessous est strictement identique dans :
//   - internal/variables/redis_ttl_constants.go (cartographie des TTL)
//   - Section 1 : déclarations des variables *Collection
//   - Section 2 : InitCacheDatabase()
//   - Section 3 : RefreshStandardTTL()

var (
	// --- FAMILLE 1 : OBJECT Cache (Tier 2 — Pool RAM éphémère non-répliqué) ---
	Users                 *Collection
	UserSettings          *Collection
	Posts                 *Collection
	Comments              *Collection
	Media                 *Collection
	Messages              *Collection
	Conversations         *Collection
	Members               *Collection
	Relations             *Collection
	Saved                 *Collection // ZSET des favoris de l'utilisateur
	PostComments          *Collection // ZSET des commentaires d'un post
	UserTimeline          *Collection // ZSET de la timeline d'un utilisateur
	NotificationsObj      *Collection
	MessageReactionCounts *Collection // Hash : msg_id -> emoji -> count
	MessageUserReactions  *Collection // Hash : msg_id -> user_id -> emoji
	POIReviews            *Collection // OBJECT (MsgPack) des avis/notes de POI
	POIReviewsByPOI       *Collection // ZSET des avis textuels par POI
	POIReviewUserIndex    *Collection // Index (poi_id:user_id -> review_id)
	POIs                  *Collection // OBJECT (MsgPack) des Points d'Intérêt (TTL 0, borné admin)

	// --- FAMILLE 2 : AUTHENTIFICATION, SESSIONS & SÉCURITÉ 2FA (Tier 1 Core) ---
	Sessions         *Collection
	Passkeys         *Collection // Object Cache L1 pour les Passkeys FIDO2
	SessionIndexes   *Collection
	SessionBlacklist *Collection
	PreAuth          *Collection // Sessions éphémères de challenge 2FA (TTL 5m)

	// --- FAMILLE 3 : SPEED Cache — Graphes, Autocomplétion & UI (Tier 1 Core) ---
	UsersLite           *Collection
	UsersLex            *Collection // ZSET lexicographique pour l'auto-complétion
	CommunitiesLex      *Collection // ZSET lexicographique pour l'auto-complétion
	TagsLex             *Collection // ZSET lexicographique pour l'auto-complétion
	SpeedRelationsIndex *Collection
	SpeedRelations      *Collection
	ConvMeta            *Collection
	ConvMembers         *Collection
	SpeedAddable        *Collection
	SpeedCommunity      *Collection

	// --- FAMILLE 4 : FEED Cache & FILTRE CUCKOO (Tier 1 Core) ---
	FeedsObject       *Collection
	FeedsPersonalized *Collection
	FeedsMailbox      *Collection
	FeedSchedule      *Collection
	CuckooSeen        *Collection

	// --- FAMILLE 5 : ALGORITHME, MOST Cache, TAGS & LSH (Tier 1 Core) ---
	ContentVectors       *Collection
	LSHBuckets           *Collection
	RankedPosts          *Collection // ZSETs stricts (likes, views, recent)
	TagPosts             *Collection // ZSETs par tags pour l'UI (cap strict 1000)
	TrendGlobalHourly    *Collection
	TrendGlobalDaily     *Collection
	TrendTagDaily        *Collection
	TrendTagWeekly       *Collection
	HashtagLeaderboard   *Collection
	Tags                 *Collection
	HashtagCanon         *Collection
	GraphEdges           *Collection
	TagQuarantine        *Collection // SET de quarantaine Cold-Start des nouveaux tags (24h)
	IndirectTagConsensus *Collection // SET de consensus multi-auteurs des tags indirects (7j)
	AuthorPostWindows    *Collection // ZSET glissant 24h des publications par auteur (anti-flood)

	// --- FAMILLE 6 : INTERACTIONS & IDEMPOTENCE (Tier 1 Core) ---
	PostLikesSet    *Collection // SET d'idempotence des likes de posts (LongTTL 30j + réhydratation L2/L3)
	CommentLikesSet *Collection // SET d'idempotence des likes de commentaires (LongTTL 30j + réhydratation L2/L3)

	// --- FAMILLE 7 : MESSAGERIE & SYNCHRONISATION INBOX (Tier 1 Core) ---
	ConvParticipants  *Collection
	UserInbox         *Collection
	InboxActivity     *Collection
	MessagesIndex     *Collection
	ConvWatermarks    *Collection
	UserSyncLedger    *Collection // ZSET des conversations modifiées par utilisateur
	ConvMessageLedger *Collection // ZSET des messages modifiés par conversation

	// --- FAMILLE 8 : NOTIFICATIONS & ACTIVITÉ (Tier 1 Core) ---
	NotificationsZSet    *Collection
	NotificationCursors  *Collection
	NotificationActivity *Collection

	// --- FAMILLE 9 : RECOMMANDATIONS UTILISATEURS & COMMUNAUTÉS (Tier 1 Core) ---
	UserRecoListGeo       *Collection // ZSET user:reco:list:geo:{user_id}
	UserRecoListAff       *Collection // ZSET user:reco:list:aff:{user_id}
	UserRecoListTaste     *Collection // ZSET user:reco:list:taste:{user_id}
	UserRecoListGlobal    *Collection // ZSET user:reco:list:global:{user_id}
	UsersLang             *Collection // SET  users:lang:{lang}
	CuckooUserRecoSkips   *Collection // CUCKOO FILTER cuckoo:user_reco:skips:{user_id}
	LockUserRecoCompute   *Collection // LOCK lock:user_reco:compute:{user_id}
	CommunityRecoRanked   *Collection // ZSET community:reco:ranked:{lang}
	CommunityRecoPins     *Collection // HASH community:reco:pins:{lang}
	CommunityRecoMsgCount *Collection // HASH community:reco:msg_count:{YYYY-MM-DD}
	CommunityRecoAuthors  *Collection // HLL  community:reco:authors:{conv_id}:{YYYY-MM-DD}
	CommunityRecoGrowth   *Collection // HASH community:reco:growth:{YYYY-MM-DD}
	UserPublicComms       *Collection // SET  user:public_comms:{user_id}

	// --- FAMILLE 10 : MODÉRATION, TRUST SCORE & CARTE DISCOVERY (Tier 1 Core) ---
	ReportAggregators     *Collection // OBJECT (MsgPack) accumulateur mathématique de signalement par cible
	SteganoBanlist        *Collection // SET global des identifiants stéganographiques bannis
	ModerationQueues      *Collection // ZSETs des files de modération ("active", "peer_review", "final_vote", "admin")
	ModerationExpirations *Collection // ZSETs des échéances temporelles ("locks" 48h, "waiting_info" 7j)
	ModerationTickets     *Collection // OBJECT (MsgPack) état de cycle de vie des tickets de modération
	ModeratorStats        *Collection // OBJECT (MsgPack) profil de fiabilité modérateur (T_mod, blâmes, audits)
	POIsGeo               *Collection // GEO / ZSET spatial global des Points d'Intérêt (TTL 0)

	// --- FAMILLE 11 : TÉLÉMÉTRIE, SYSTÈME, QUEUES & SCHEDULER (Tier 1 Core) ---
	TelemetryProfiles *Collection
	RateLimits        *Collection
	CronLocks         *Collection // Verrous distribués atomiques pour les Cron Workers
	InfraTelemetry    *Collection // Heartbeats & agrégats DC live (infra:telemetry:*)
	SchedulerState    *Collection // Checkpoints, pause & régime de l'Activity-Aware Scheduler
	DynamicVariables  *Collection // Configuration dynamique centralisée (Hash tag Cluster-safe)
	WorkerQueue       *Collection // Queue FIFO Write-Behind vers L2/L3
	DLQ               *Collection // Dead Letter Queue des erreurs Postgres
	SystemStatus      *Collection

	// --- FAMILLE 12 : BUS PUB/SUB & PRÉSENCE TEMPS RÉEL (Tier 3 PubSub) ---
	Presence             *Collection
	ChannelUser          *Collection // Pub/Sub pur (0 octet en RAM)
	ChannelCommunity     *Collection // Pub/Sub pur (0 octet en RAM)
	CuckooSync           *Collection // Claim-Check Pub/Sub inter-pods
	DynamicVariablesSync *Collection // Claim-Check Pub/Sub inter-pods
)

// ============================================================================
// 2. INITIALISATION (ARCHITECTURE REDIS 3 TIERS — ZÉRO TTL HARDCODÉ)
// ============================================================================

// InitCacheDatabase instancie l'ensemble des 100 collections Redis sur leurs tiers
// respectifs (Core HA, Object Pool, Pub/Sub) en utilisant exclusivement les TTL
// centralisés de internal/variables/.
func InitCacheDatabase() {
	// --- FAMILLE 1 : OBJECT Cache (Tier 2 — Pool RAM éphémère non-répliqué, repli L2/L3) ---
	Users = newObjectCollection("object_cache:user", variables.StandardTTL)
	UserSettings = newObjectCollection("object_cache:user_settings", variables.StandardTTL)
	Posts = newObjectCollection("object_cache:post_service", variables.StandardTTL)
	Comments = newObjectCollection("object_cache:comment", variables.StandardTTL)
	Media = newObjectCollection("object_cache:media", variables.StandardTTL)
	Messages = newObjectCollection("object_cache:msg", variables.StandardTTL)
	Conversations = newObjectCollection("object_cache:conv", variables.StandardTTL)
	Members = newObjectCollection("object_cache:member", variables.StandardTTL)
	Relations = newObjectCollection("object_cache:rel", variables.StandardTTL)
	Saved = newObjectCollection("object_cache:saved:zset", variables.StandardTTL)
	PostComments = newObjectCollection("object:comments:zset", variables.StandardTTL)
	UserTimeline = newObjectCollection("user_cache:posts:zset", variables.StandardTTL)
	NotificationsObj = newObjectCollection("object_cache:notification", variables.StandardTTL)
	MessageReactionCounts = newObjectCollection("msg_react_counts", variables.StandardTTL)
	MessageUserReactions = newObjectCollection("msg_user_react", variables.StandardTTL)
	POIReviews = newObjectCollection("object_cache:poi_review", variables.StandardTTL)
	POIReviewsByPOI = newObjectCollection("object_cache:poi_reviews:zset", variables.StandardTTL)
	POIReviewUserIndex = newObjectCollection("index:poi_review:user", variables.StandardTTL)
	POIs = newObjectCollection("object_cache:poi", variables.NoTTL)

	// --- FAMILLE 2 : AUTHENTIFICATION, SESSIONS & SÉCURITÉ 2FA (Tier 1 Core) ---
	Sessions = newCoreCollection("object_cache:session", variables.StandardTTL)
	Passkeys = newCoreCollection("object_cache:passkey", variables.StandardTTL)
	SessionIndexes = newCoreCollection("session_cache", variables.StandardTTL)
	SessionBlacklist = newCoreCollection("blacklist:session", variables.ShortTTL)
	PreAuth = newCoreCollection("auth:pre_auth", variables.TwoFactorPreAuthTTL)

	// --- FAMILLE 3 : SPEED Cache — Graphes, Autocomplétion & UI (Tier 1 Core) ---
	UsersLite = newCoreCollection("speed_cache:user:lite", variables.NoTTL)
	UsersLex = newCoreCollection("speed_cache:user:search", variables.NoTTL)
	CommunitiesLex = newCoreCollection("speed_cache:community:search", variables.NoTTL)
	TagsLex = newCoreCollection("speed_cache:tag:search", variables.NoTTL)
	SpeedRelationsIndex = newCoreCollection("speed_cache:relations:index:default", variables.NoTTL)
	SpeedRelations = newCoreCollection("speed_cache:relations:default", variables.NoTTL)
	ConvMeta = newCoreCollection("speed_cache:conversation:meta", variables.StandardTTL)
	ConvMembers = newCoreCollection("speed_cache:conversation:members", variables.StandardTTL)
	SpeedAddable = newCoreCollection("speed_cache:addable:default", variables.StandardTTL)
	SpeedCommunity = newCoreCollection("speed_cache:community:default", variables.StandardTTL)

	// --- FAMILLE 4 : FEED Cache & FILTRE CUCKOO (Tier 1 Core) ---
	FeedsObject = newCoreCollection("feed:state", variables.ShortTTL)
	FeedsPersonalized = newCoreCollection("feed:personalized", variables.ShortTTL)
	FeedsMailbox = newCoreCollection("feed_cache:mailbox", variables.StandardTTL)
	FeedSchedule = newCoreCollection("feed:precompute:schedule", variables.NoTTL)
	CuckooSeen = newCoreCollection("cuckoo:seen", variables.ShortTTL)

	// --- FAMILLE 5 : ALGORITHME, MOST Cache, TAGS & LSH (Tier 1 Core) ---
	ContentVectors = newCoreCollection("most_cache:vec", variables.StandardTTL)
	LSHBuckets = newCoreCollection("lsh:bucket", variables.StandardTTL)
	RankedPosts = newCoreCollection("most_cache", variables.NoTTL)
	TagPosts = newCoreCollection("most_cache:idx:tag", variables.NoTTL)
	TrendGlobalHourly = newCoreCollection("trend:global:hourly", variables.TrendGlobalHourlyTTL)
	TrendGlobalDaily = newCoreCollection("trend:global:daily", variables.TrendGlobalDailyTTL)
	TrendTagDaily = newCoreCollection("trend:tag:daily", variables.TrendTagDailyTTL)
	TrendTagWeekly = newCoreCollection("trend:tag:weekly", variables.TrendTagWeeklyTTL)
	HashtagLeaderboard = newCoreCollection("hashtag:leaderboard", variables.NoTTL)
	Tags = newCoreCollection("tags", variables.NoTTL)
	HashtagCanon = newCoreCollection("hashtag:canon", variables.NoTTL)
	GraphEdges = newCoreCollection("graph_cache:tag_edges", variables.NoTTL)
	TagQuarantine = newCoreCollection("tags:quarantine", variables.ShortTTL)
	IndirectTagConsensus = newCoreCollection("post:itags:consensus", variables.StandardTTL)
	AuthorPostWindows = newCoreCollection("author:posts:window", variables.AuthorPostWindowTTL)

	// --- FAMILLE 6 : INTERACTIONS & IDEMPOTENCE (Tier 1 Core) ---
	PostLikesSet = newCoreCollection("post:likes_set", variables.LongTTL)
	CommentLikesSet = newCoreCollection("comment:likes_set", variables.LongTTL)

	// --- FAMILLE 7 : MESSAGERIE & SYNCHRONISATION INBOX (Tier 1 Core) ---
	ConvParticipants = newCoreCollection("conv:participants", variables.NoTTL)
	UserInbox = newCoreCollection("inbox:user", variables.NoTTL)
	InboxActivity = newCoreCollection("inbox:activity", variables.NoTTL)
	MessagesIndex = newCoreCollection("messages:idx", variables.StandardTTL)
	ConvWatermarks = newCoreCollection("conv:watermarks", variables.StandardTTL)
	UserSyncLedger = newCoreCollection("sync:ledger:user", variables.LongTTL)
	ConvMessageLedger = newCoreCollection("sync:ledger:conv_msg", variables.LongTTL)

	// --- FAMILLE 8 : NOTIFICATIONS & ACTIVITÉ (Tier 1 Core) ---
	NotificationsZSet = newCoreCollection("notifications:user", variables.StandardTTL)
	NotificationCursors = newCoreCollection("notifications:cursor", variables.NoTTL)
	NotificationActivity = newCoreCollection("notifications:activity", variables.NoTTL)

	// --- FAMILLE 9 : RECOMMANDATIONS UTILISATEURS & COMMUNAUTÉS (Tier 1 Core) ---
	UserRecoListGeo = newCoreCollection(variables.RedisKeyUserRecoListGeo, variables.UserRecoListTTL)
	UserRecoListAff = newCoreCollection(variables.RedisKeyUserRecoListAff, variables.UserRecoListTTL)
	UserRecoListTaste = newCoreCollection(variables.RedisKeyUserRecoListTaste, variables.UserRecoListTTL)
	UserRecoListGlobal = newCoreCollection(variables.RedisKeyUserRecoListGlobal, variables.UserRecoListTTL)
	UsersLang = newCoreCollection(variables.RedisKeyUsersLang, variables.NoTTL)
	CuckooUserRecoSkips = newCoreCollection(variables.RedisKeyCuckooUserRecoSkips, variables.StandardTTL)
	LockUserRecoCompute = newCoreCollection(variables.RedisKeyLockUserRecoCompute, variables.UserRecoComputeLockTTL)
	CommunityRecoRanked = newCoreCollection(variables.RedisKeyCommunityRecoRanked, variables.NoTTL)
	CommunityRecoPins = newCoreCollection(variables.RedisKeyCommunityRecoPins, variables.NoTTL)
	CommunityRecoMsgCount = newCoreCollection(variables.RedisKeyCommunityRecoMsgCount, variables.CommunityRecoTelemetryTTL)
	CommunityRecoAuthors = newCoreCollection(variables.RedisKeyCommunityRecoAuthors, variables.CommunityRecoTelemetryTTL)
	CommunityRecoGrowth = newCoreCollection(variables.RedisKeyCommunityRecoGrowth, variables.CommunityRecoTelemetryTTL)
	UserPublicComms = newCoreCollection(variables.RedisKeyUserPublicComms, variables.NoTTL)

	// --- FAMILLE 10 : MODÉRATION, TRUST SCORE & CARTE DISCOVERY (Tier 1 Core) ---
	ReportAggregators = newCoreCollection("moderation:report_agg", variables.StandardTTL)
	SteganoBanlist = newCoreCollection("moderation:stegano_banlist", variables.NoTTL)
	ModerationQueues = newCoreCollection("moderation:queue", variables.NoTTL)
	ModerationExpirations = newCoreCollection("moderation:expirations", variables.NoTTL)
	ModerationTickets = newCoreCollection("moderation:ticket", variables.NoTTL)
	ModeratorStats = newCoreCollection("moderation:mod_stats", variables.NoTTL)
	POIsGeo = newCoreCollection("geo_cache:poi", variables.NoTTL)

	// --- FAMILLE 11 : TÉLÉMÉTRIE, SYSTÈME, QUEUES & SCHEDULER (Tier 1 Core) ---
	TelemetryProfiles = newCoreCollection("telemetry:profiles", variables.StandardTTL)
	RateLimits = newCoreCollection("rate_limit:ip", variables.UltraShortTTL)
	CronLocks = newCoreCollection("lock:cron", variables.VeryShortTTL)
	InfraTelemetry = newCoreCollection("infra:telemetry", variables.MinuteTTL)
	SchedulerState = newCoreCollection("scheduler", variables.NoTTL)
	// Hash tag {dynamic_vars} garantit que KEYS[1] (overrides) et KEYS[2] (version)
	// atterrissent toujours sur le même slot Redis Cluster (zéro CROSSSLOT Lua).
	DynamicVariables = newCoreCollection("system:{dynamic_vars}:variables", variables.NoTTL)
	WorkerQueue = newCoreCollection("worker:queue", variables.NoTTL)
	DLQ = newCoreCollection("dlq", variables.NoTTL)
	SystemStatus = newCoreCollection("system:status", variables.NoTTL)

	// --- FAMILLE 12 : BUS PUB/SUB & PRÉSENCE TEMPS RÉEL (Tier 3 PubSub) ---
	Presence = newPubSubCollection("presence:user", variables.MinuteTTL)
	ChannelUser = newPubSubCollection("channel:user", variables.NoTTL)
	ChannelCommunity = newPubSubCollection("channel:community", variables.NoTTL)
	CuckooSync = newPubSubCollection("cuckoo:sync", variables.UltraShortTTL)
	DynamicVariablesSync = newPubSubCollection("system:variables:sync", variables.UltraShortTTL)
}

// ============================================================================
// 3. RAFRAÎCHISSEMENT DYNAMIQUE DE StandardTTL (HOT-RELOAD ADMIN)
// ============================================================================

// RefreshStandardTTL met à jour dynamiquement le DefaultTTL de toutes les collections
// indexées sur StandardTTL lorsqu'un administrateur modifie StandardTTL à chaud.
func RefreshStandardTTL(newTTL time.Duration) {
	collections := []*Collection{
		// Famille 1 : OBJECT Cache (18 collections)
		Users, UserSettings, Posts, Comments, Media, Messages, Conversations,
		Members, Relations, Saved, PostComments, UserTimeline, NotificationsObj,
		MessageReactionCounts, MessageUserReactions, POIReviews, POIReviewsByPOI,
		POIReviewUserIndex,
		// Famille 2 : Auth & Sessions (3 collections)
		Sessions, Passkeys, SessionIndexes,
		// Famille 3 : SPEED Cache (4 collections)
		ConvMeta, ConvMembers, SpeedAddable, SpeedCommunity,
		// Famille 4 : FEED Cache (1 collection)
		FeedsMailbox,
		// Famille 5 : Algorithme, MOST Cache & Tags (3 collections)
		ContentVectors, LSHBuckets, IndirectTagConsensus,
		// Famille 7 : Messagerie (2 collections)
		MessagesIndex, ConvWatermarks,
		// Famille 8 : Notifications (1 collection)
		NotificationsZSet,
		// Famille 9 : Recommandations (1 collection)
		CuckooUserRecoSkips,
		// Famille 10 : Modération (1 collection)
		ReportAggregators,
		// Famille 11 : Télémétrie (1 collection)
		TelemetryProfiles,
	}
	for _, col := range collections {
		if col != nil {
			col.DefaultTTL = newTTL
		}
	}
}
