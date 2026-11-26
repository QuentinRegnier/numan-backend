package variables

import "time"

// ============================================================================
// 1. PALIERS DE TTL REDIS GÉNÉRIQUES (SOURCE DE VÉRITÉ CENTRALISÉE)
// ============================================================================
//
// Toute collection Redis définie dans internal/repository/redis/collections.go
// doit référencer l'un de ces paliers, une constante métier mathématique dédiée
// (voir section 2) ou NoTTL (0) pour les structures persistantes / non-évictables.
// Aucun littéral time.Duration ne doit être hardcodé dans collections.go.

const (
	// NoTTL désactive l'expiration Redis (TTL = 0) pour les socles critiques,
	// index structurels bornés, queues Write-Behind et canaux Pub/Sub purs.
	NoTTL time.Duration = 0

	// UltraShortTTL (10s) : Rate-limiting IP et synchronisation Claim-Check Pub/Sub
	// inter-pods (CuckooSync, DynamicVariablesSync).
	UltraShortTTL = 10 * time.Second

	// MinuteTTL (60s) : Heartbeats temps réel éphémères (Presence, InfraTelemetry).
	MinuteTTL = 60 * time.Second

	// VeryShortTTL (5m) : Challenges 2FA éphémères (PreAuth) et verrous distribués (CronLocks).
	VeryShortTTL = 5 * time.Minute

	// ShortTTL (24h) : Caches éphémères journaliers (SessionBlacklist, FeedsObject,
	// FeedsPersonalized, CuckooSeen, TagQuarantine).
	ShortTTL = 24 * time.Hour

	// LongTTL (30j) : Journaux de synchronisation delta longue durée (UserSyncLedger,
	// ConvMessageLedger) et sets d'idempotence des likes avec réhydratation L1<-L2<-L3
	// (PostLikesSet, CommentLikesSet).
	LongTTL = 30 * 24 * time.Hour
)

// StandardTTL (7j) est modifiable à chaud via l'API admin (/admin/variables) ;
// toute modification déclenche automatiquement redis.RefreshStandardTTL via SetStandardTTLHook.
var (
	StandardTTL = 7 * 24 * time.Hour // TTL STANDARD : 7 Jours
)

// ============================================================================
// 2. CARTOGRAPHIE DES TTL PAR FAMILLE DE COLLECTIONS REDIS
// ============================================================================
//
// L'ordre des 12 familles ci-dessous reflète exactement la déclaration et
// l'initialisation des 101 collections dans internal/repository/redis/collections.go.
//
// Famille 1  — OBJECT Cache (Tier 2)                 : StandardTTL (7j) | POIs: NoTTL (0)
// Famille 2  — Auth, Sessions & 2FA (Tier 1)         : Sessions/Passkeys/SessionIndexes: StandardTTL (7j)
//                                                      SessionBlacklist: ShortTTL (24h)
//                                                      PreAuth: TwoFactorPreAuthTTL (5m = VeryShortTTL)
// Famille 3  — SPEED Cache (Tier 1)                  : UsersLite/Lex/Relations*: NoTTL (0)
//                                                      ConvMeta/ConvMembers/SpeedAddable/SpeedCommunity: StandardTTL (7j)
// Famille 4  — FEED Cache & Cuckoo (Tier 1)          : FeedsObject/FeedsPersonalized/CuckooSeen/FanOutAuthorWindows: ShortTTL (24h)
//                                                      FeedsMailbox: StandardTTL (7j) | FeedSchedule: NoTTL (0)
// Famille 5  — Algorithme, MOST Cache & Tags (Tier 1): ContentVectors/LSHBuckets/IndirectTagConsensus: StandardTTL (7j)
//                                                      RankedPosts/TagPosts/HashtagLeaderboard/Tags/HashtagCanon/GraphEdges: NoTTL (0)
//                                                      TrendGlobalHourlyTTL (48h), TrendGlobalDailyTTL (72h),
//                                                      TrendTagDailyTTL (72h), TrendTagWeeklyTTL (14j),
//                                                      TagQuarantine: ShortTTL (24h), AuthorPostWindowTTL (24h)
// Famille 6  — Interactions & Idempotence (Tier 1)   : PostLikesSet/CommentLikesSet: LongTTL (30j + réhydratation L2/L3)
// Famille 7  — Messagerie & Sync Inbox (Tier 1)      : ConvParticipants/UserInbox/InboxActivity: NoTTL (0)
//                                                      MessagesIndex/ConvWatermarks: StandardTTL (7j)
//                                                      UserSyncLedger/ConvMessageLedger: LongTTL (30j)
// Famille 8  — Notifications & Activité (Tier 1)     : NotificationsZSet: StandardTTL (7j)
//                                                      NotificationCursors/NotificationActivity: NoTTL (0)
// Famille 9  — Recommandations Users/Comms (Tier 1)  : UserRecoListTTL (48h), LockUserRecoCompute (12h),
//                                                      CuckooUserRecoSkips: StandardTTL (7j), UsersLang: NoTTL (0),
//                                                      CommunityRecoRanked/Pins/UserPublicComms: NoTTL (0),
//                                                      CommunityRecoTelemetryTTL (8j)
// Famille 10 — Modération, Trust & Geo (Tier 1)      : ReportAggregators: StandardTTL (7j)
//                                                      SteganoBanlist/Queues/Expirations/Tickets/Stats/POIsGeo: NoTTL (0)
// Famille 11 — Télémétrie, Système & Queues (Tier 1) : TelemetryProfiles: StandardTTL (7j), RateLimits: UltraShortTTL (10s),
//                                                      CronLocks: VeryShortTTL (5m), InfraTelemetry: MinuteTTL (60s),
//                                                      SchedulerState/DynamicVariables/WorkerQueue/DLQ/SystemStatus: NoTTL (0)
// Famille 12 — Bus Pub/Sub & Présence (Tier 3)       : Presence: MinuteTTL (60s), CuckooSync/DynamicVariablesSync: UltraShortTTL (10s),
//                                                      ChannelUser/ChannelCommunity: NoTTL (0)
