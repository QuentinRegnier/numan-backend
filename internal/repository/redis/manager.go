package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/redis/go-redis/v9"
	"github.com/vmihailenco/msgpack/v5"
)

// ============================================================================
// 1. VARIABLES GLOBALES (LES COLLECTIONS DE L'APPLICATION)
// ============================================================================

var (
	// --- OBJECT Cache (JSON/MsgPack + TTL) ---
	Users            *Collection
	UserSettings     *Collection
	Sessions         *Collection
	Passkeys         *Collection // Object Cache L1 pour les Passkeys FIDO2
	Posts            *Collection
	Comments         *Collection
	Media            *Collection
	Conversations    *Collection
	Members          *Collection
	Messages         *Collection
	Relations        *Collection
	Saved            *Collection // ZSET des favoris de l'utilisateur
	PostComments     *Collection // ZSET des commentaires d'un post
	UserTimeline     *Collection // ZSET de la timeline d'un utilisateur
	NotificationsObj *Collection

	// --- SPEED Cache (Lite Objects & UI) ---
	UsersLite           *Collection
	UsersLex            *Collection // ZSET Lexicographique pour l'auto-complétion
	CommunitiesLex      *Collection // ZSET Lexicographique pour l'auto-complétion
	TagsLex             *Collection // ZSET Lexicographique pour l'auto-complétion
	ConvMeta            *Collection
	ConvMembers         *Collection
	SpeedRelationsIndex *Collection
	SpeedRelations      *Collection
	SpeedAddable        *Collection
	SpeedCommunity      *Collection

	// --- FEED Cache ---
	FeedsObject       *Collection
	FeedsMailbox      *Collection
	FeedsPersonalized *Collection

	// --- ALGORITHM & MOST Cache ---
	ContentVectors       *Collection
	RankedPosts          *Collection // Les ZSETs stricts (likes, views, recent)
	TagPosts             *Collection // Les ZSETs par tags pour l'UI
	TrendGlobalHourly    *Collection
	TrendGlobalDaily     *Collection
	TrendTagDaily        *Collection
	TrendTagWeekly       *Collection
	HashtagLeaderboard   *Collection
	TagQuarantine        *Collection // SET de quarantaine Cold-Start des nouveaux tags (24h)
	IndirectTagConsensus *Collection // SET de consensus multi-auteurs des tags indirects (7j)
	AuthorPostWindows    *Collection // ZSET glissant 24h des publications par auteur (anti-flood)

	// --- SYSTEM Cache ---
	RateLimits   *Collection
	DLQ          *Collection
	GraphEdges   *Collection
	Tags         *Collection
	HashtagCanon *Collection
	CronLocks    *Collection // Verrous distribués atomiques pour les Cron Workers

	// --- INDEX & IDEMPOTENCE ---
	SessionIndexes   *Collection
	SessionBlacklist *Collection
	PostLikesSet     *Collection
	CommentLikesSet  *Collection
	SystemStatus     *Collection

	// --- CUCKOO FILTER ---
	CuckooSeen *Collection
	CuckooSync *Collection // NOUVEAU

	// --- MESSAGING & LSH ---
	ConvParticipants *Collection
	UserInbox        *Collection
	MessagesIndex    *Collection
	LSHBuckets       *Collection
	ConvWatermarks   *Collection

	UserSyncLedger    *Collection // ZSET des conversations modifiées par utilisateur
	ConvMessageLedger *Collection // ZSET des messages modifiés par conversation

	// --- RÉACTIONS AUX MESSAGES ---
	MessageReactionCounts *Collection // Hash: msg_id -> emoji -> count
	MessageUserReactions  *Collection // Hash: msg_id -> user_id -> emoji

	// --- ACTIVITY FEED ---
	FeedSchedule         *Collection
	NotificationsZSet    *Collection
	NotificationCursors  *Collection
	InboxActivity        *Collection
	NotificationActivity *Collection

	// --- TELEMETRY Cache ---
	TelemetryProfiles *Collection

	// --- WEBSOCKET Cache ---
	Presence *Collection

	// --- PUB/SUB CHANNELS & WORKER QUEUES ---
	ChannelUser      *Collection
	ChannelCommunity *Collection
	WorkerQueue      *Collection

	// --- CONFIGURATION DYNAMIQUE CENTRALISÉE ---
	DynamicVariables     *Collection
	DynamicVariablesSync *Collection

	// --- PASSKEY TWO FACTOR ---
	PreAuth *Collection // Sessions éphémères de challenge 2FA (TTL 5m)

	// --- TRUST SCORE & MODERATION ---
	ReportAggregators     *Collection // OBJECT (MsgPack) de l'accumulateur mathématique de signalement par cible
	SteganoBanlist        *Collection // SET global des identifiants stéganographiques bannis
	ModerationQueues      *Collection // ZSETs des files de modération ("active", "peer_review", "final_vote", "admin")
	ModerationExpirations *Collection // ZSETs des échéances temporelles ("locks" 48h, "waiting_info" 7j)
	ModerationTickets     *Collection // OBJECT (MsgPack) de l'état de cycle de vie des tickets de modération
	ModeratorStats        *Collection // OBJECT (MsgPack) du profil de fiabilité modérateur (T_mod, blâmes, audits)

	// --- DISCOVERY MAP & POINTS OF INTEREST (TTL 0 — Non-expiring) ---
	POIs               *Collection // OBJECT (MsgPack) des Points d'Intérêt (TTL 0)
	POIsGeo            *Collection // GEO / ZSET spatial global des Points d'Intérêt (TTL 0)
	POIReviews         *Collection // OBJECT (MsgPack) des avis/notes de POI (StandardTTL)
	POIReviewsByPOI    *Collection // ZSET des avis textuels par POI (StandardTTL)
	POIReviewUserIndex *Collection // Index (poi_id:user_id -> review_id) pour garantir 1 avis par utilisateur (StandardTTL)

	// --- USER RECOMMENDATION SYSTEM (L1 Speed Cache) ---
	UserRecoListGeo     *Collection // ZSET user:reco:list:geo:{user_id}
	UserRecoListAff     *Collection // ZSET user:reco:list:aff:{user_id}
	UserRecoListTaste   *Collection // ZSET user:reco:list:taste:{user_id}
	UserRecoListGlobal  *Collection // ZSET user:reco:list:global:{user_id}
	UsersLang           *Collection // SET users:lang:{lang}
	CuckooUserRecoSkips *Collection // CUCKOO FILTER cuckoo:user_reco:skips:{user_id}
	LockUserRecoCompute *Collection // LOCK lock:user_reco:compute:{user_id}

	// --- PUBLIC COMMUNITY RECOMMENDATION SYSTEM (L1 Speed Cache & Telemetry) ---
	CommunityRecoRanked   *Collection // ZSET community:reco:ranked:{lang}
	CommunityRecoPins     *Collection // HASH community:reco:pins:{lang}
	CommunityRecoMsgCount *Collection // HASH community:reco:msg_count:{YYYY-MM-DD}
	CommunityRecoAuthors  *Collection // HLL  community:reco:authors:{conv_id}:{YYYY-MM-DD}
	CommunityRecoGrowth   *Collection // HASH community:reco:growth:{YYYY-MM-DD}
	UserPublicComms       *Collection // SET  user:public_comms:{user_id}

	// --- INFRA TELEMETRY & ACTIVITY-AWARE SCHEDULER (Tier 1 Core) ---
	InfraTelemetry *Collection // Telemetry heartbeats & live DC aggregates (infra:telemetry:*)
	SchedulerState *Collection // Scheduler job checkpoints, pause state & regime (scheduler:*)
)

// ============================================================================
// 2. INITIALISATION (ARCHITECTURE REDIS 3 TIERS)
// ============================================================================

func InitCacheDatabase() {
	// --- TIER 2 : OBJECT Cache (100% RAM éphémère non-répliquée, repli MongoDB L2) ---
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
	POIs = newObjectCollection("object_cache:poi", 0)
	POIReviews = newObjectCollection("object_cache:poi_review", variables.StandardTTL)
	POIReviewsByPOI = newObjectCollection("object_cache:poi_reviews:zset", variables.StandardTTL)
	POIReviewUserIndex = newObjectCollection("index:poi_review:user", variables.StandardTTL)
	MessageReactionCounts = newObjectCollection("msg_react_counts", variables.StandardTTL)
	MessageUserReactions = newObjectCollection("msg_user_react", variables.StandardTTL)

	// --- TIER 1 : CORE HA (Auth, Sessions, Locks, Speed Cache, MOST Cache, Write-Behind, Telemetry) ---
	Sessions = newCoreCollection("object_cache:session", variables.StandardTTL)
	Passkeys = newCoreCollection("object_cache:passkey", variables.StandardTTL)

	// --- SPEED Cache (Tier 1 Core) ---
	UsersLite = newCoreCollection("speed_cache:user:lite", 0)
	UsersLex = newCoreCollection("speed_cache:user:search", 0)
	CommunitiesLex = newCoreCollection("speed_cache:community:search", 0)
	TagsLex = newCoreCollection("speed_cache:tag:search", 0)
	ConvMeta = newCoreCollection("speed_cache:conversation:meta", variables.StandardTTL)
	ConvMembers = newCoreCollection("speed_cache:conversation:members", variables.StandardTTL)
	SpeedRelationsIndex = newCoreCollection("speed_cache:relations:index:default", 0)
	SpeedRelations = newCoreCollection("speed_cache:relations:default", 0)
	SpeedAddable = newCoreCollection("speed_cache:addable:default", variables.StandardTTL)
	SpeedCommunity = newCoreCollection("speed_cache:community:default", variables.StandardTTL)

	// --- FEED Cache (Tier 1 Core) ---
	FeedsObject = newCoreCollection("feed:state", variables.StandardTTL)
	FeedsMailbox = newCoreCollection("feed_cache:mailbox", variables.StandardTTL)
	FeedsPersonalized = newCoreCollection("feed:personalized", variables.StandardTTL)

	// --- ALGORITHM & MOST Cache (Tier 1 Core) ---
	ContentVectors = newCoreCollection("most_cache:vec", variables.StandardTTL)
	RankedPosts = newCoreCollection("most_cache", variables.StandardTTL)
	TagPosts = newCoreCollection("most_cache:idx:tag", variables.StandardTTL)
	TrendGlobalHourly = newCoreCollection("trend:global:hourly", variables.TrendGlobalHourlyTTL)
	TrendGlobalDaily = newCoreCollection("trend:global:daily", variables.TrendGlobalDailyTTL)
	TrendTagDaily = newCoreCollection("trend:tag:daily", variables.TrendTagDailyTTL)
	TrendTagWeekly = newCoreCollection("trend:tag:weekly", variables.TrendTagWeeklyTTL)
	HashtagLeaderboard = newCoreCollection("hashtag:leaderboard", 0)
	TagQuarantine = newCoreCollection("tags:quarantine", variables.TagQuarantineTTL)
	IndirectTagConsensus = newCoreCollection("post:itags:consensus", variables.IndirectTagSetTTL)
	AuthorPostWindows = newCoreCollection("author:posts:window", variables.AuthorPostWindowTTL)

	// --- SYSTEM Cache (Tier 1 Core) ---
	RateLimits = newCoreCollection("rate_limit:ip", 10*time.Second)
	DLQ = newCoreCollection("dlq", 0)
	GraphEdges = newCoreCollection("graph_cache:tag_edges", 0)
	Tags = newCoreCollection("tags", 0)
	HashtagCanon = newCoreCollection("hashtag:canon", 0)
	CronLocks = newCoreCollection("lock:cron", 5*time.Minute)

	// --- INDEX & IDEMPOTENCE (Tier 1 Core) ---
	SessionIndexes = newCoreCollection("session_cache", variables.StandardTTL)
	SessionBlacklist = newCoreCollection("blacklist:session", 24*time.Hour)
	PostLikesSet = newCoreCollection("post:likes_set", 0)
	CommentLikesSet = newCoreCollection("comment:likes_set", 0)
	SystemStatus = newCoreCollection("system:status", 0)

	// --- CUCKOO FILTER ---
	CuckooSeen = newCoreCollection("cuckoo:seen", variables.StandardTTL)
	CuckooSync = newPubSubCollection("cuckoo:sync", 5*time.Second) // Tier 3 PubSub Bus

	// --- MESSAGING & LSH (Tier 1 Core) ---
	ConvParticipants = newCoreCollection("conv:participants", 0)
	UserInbox = newCoreCollection("inbox:user", 0)
	MessagesIndex = newCoreCollection("messages:idx", 24*time.Hour)
	LSHBuckets = newCoreCollection("lsh:bucket", variables.StandardTTL)
	ConvWatermarks = newCoreCollection("conv:watermarks", variables.StandardTTL)

	UserSyncLedger = newCoreCollection("sync:ledger:user", 30*24*time.Hour)
	ConvMessageLedger = newCoreCollection("sync:ledger:conv_msg", 30*24*time.Hour)

	// --- ACTIVITY FEED (Tier 1 Core) ---
	FeedSchedule = newCoreCollection("feed:precompute:schedule", 0)
	NotificationsZSet = newCoreCollection("notifications:user", variables.StandardTTL)
	NotificationCursors = newCoreCollection("notifications:cursor", 0)
	NotificationActivity = newCoreCollection("notifications:activity", 0)
	InboxActivity = newCoreCollection("inbox:activity", 0)

	// --- TELEMETRY Cache (Tier 1 Core) ---
	TelemetryProfiles = newCoreCollection("telemetry:profiles", variables.StandardTTL)

	// --- TIER 3 : PUB/SUB & EPHEMERAL PRESENCE BUS ---
	Presence = newPubSubCollection("presence:user", 60*time.Second)
	ChannelUser = newPubSubCollection("channel:user", 0)
	ChannelCommunity = newPubSubCollection("channel:community", 0)

	// --- WORKER QUEUES (Tier 1 Core) ---
	WorkerQueue = newCoreCollection("worker:queue", 0)

	// --- CONFIGURATION DYNAMIQUE CENTRALISÉE ---
	// Hash tag {dynamic_vars} garantit que KEYS[1] (overrides) et KEYS[2] (version)
	// atterrissent toujours sur le même slot Redis Cluster (zéro CROSSSLOT Lua).
	DynamicVariables = newCoreCollection("system:{dynamic_vars}:variables", 0)
	DynamicVariablesSync = newPubSubCollection("system:variables:sync", 10*time.Second) // Tier 3 PubSub Bus

	// --- PASSKEY TWO FACTOR (Tier 1 Core) ---
	PreAuth = newCoreCollection("auth:pre_auth", variables.TwoFactorPreAuthTTL)

	// --- TRUST SCORE & MODERATION (Tier 1 Core) ---
	ReportAggregators = newCoreCollection("moderation:report_agg", variables.StandardTTL)
	SteganoBanlist = newCoreCollection("moderation:stegano_banlist", 0)
	ModerationQueues = newCoreCollection("moderation:queue", 0)
	ModerationExpirations = newCoreCollection("moderation:expirations", 0)
	ModerationTickets = newCoreCollection("moderation:ticket", 0)
	ModeratorStats = newCoreCollection("moderation:mod_stats", 0)

	// --- DISCOVERY MAP GEO INDEX (Tier 1 Core) ---
	POIsGeo = newCoreCollection("geo_cache:poi", 0)

	// --- USER RECOMMENDATION SYSTEM (Tier 1 Core) ---
	UserRecoListGeo = newCoreCollection(variables.RedisKeyUserRecoListGeo, variables.UserRecoListTTL)
	UserRecoListAff = newCoreCollection(variables.RedisKeyUserRecoListAff, variables.UserRecoListTTL)
	UserRecoListTaste = newCoreCollection(variables.RedisKeyUserRecoListTaste, variables.UserRecoListTTL)
	UserRecoListGlobal = newCoreCollection(variables.RedisKeyUserRecoListGlobal, variables.UserRecoListTTL)
	UsersLang = newCoreCollection(variables.RedisKeyUsersLang, 0)
	CuckooUserRecoSkips = newCoreCollection(variables.RedisKeyCuckooUserRecoSkips, time.Duration(variables.UserRecoSkipTTLSeconds)*time.Second)
	LockUserRecoCompute = newCoreCollection(variables.RedisKeyLockUserRecoCompute, variables.UserRecoComputeLockTTL)

	// --- PUBLIC COMMUNITY RECOMMENDATION SYSTEM (Tier 1 Core) ---
	CommunityRecoRanked = newCoreCollection(variables.RedisKeyCommunityRecoRanked, 0)
	CommunityRecoPins = newCoreCollection(variables.RedisKeyCommunityRecoPins, 0)
	CommunityRecoMsgCount = newCoreCollection(variables.RedisKeyCommunityRecoMsgCount, variables.CommunityRecoTelemetryTTL)
	CommunityRecoAuthors = newCoreCollection(variables.RedisKeyCommunityRecoAuthors, variables.CommunityRecoTelemetryTTL)
	CommunityRecoGrowth = newCoreCollection(variables.RedisKeyCommunityRecoGrowth, variables.CommunityRecoTelemetryTTL)
	UserPublicComms = newCoreCollection(variables.RedisKeyUserPublicComms, 0)

	// --- INFRA TELEMETRY & ACTIVITY-AWARE SCHEDULER (Tier 1 Core) ---
	InfraTelemetry = newCoreCollection("infra:telemetry", 60*time.Second)
	SchedulerState = newCoreCollection("scheduler", 0)
}

// ============================================================================
// 3. INFRASTRUCTURE & CYCLE DE VIE
// ============================================================================

// IsReady isole l'état de l'infrastructure pour les routines de maintenance.
func IsReady() bool {
	return redisgo.CoreClient() != nil
}

// Keys renvoie toutes les clés correspondant à un pattern global (Cluster-safe via ForEachMaster).
func Keys(ctx context.Context, pattern string) ([]string, error) {
	client := redisgo.CoreClient()
	if client == nil {
		return nil, nil
	}
	if clusterClient, ok := client.(*redis.ClusterClient); ok {
		var all []string
		err := clusterClient.ForEachMaster(ctx, func(c context.Context, master *redis.Client) error {
			ks, errM := master.Keys(c, pattern).Result()
			if errM != nil {
				return errM
			}
			all = append(all, ks...)
			return nil
		})
		return all, err
	}
	return client.Keys(ctx, pattern).Result()
}

// FlushDB vide l'intégralité de la base de données Redis courante (Cluster-safe sur tous les masters).
func FlushDB(ctx context.Context) error {
	seen := make(map[redis.UniversalClient]struct{}, 3)
	for _, client := range []redis.UniversalClient{redisgo.CoreClient(), redisgo.ObjectClient(), redisgo.PubSubClient()} {
		if client == nil {
			continue
		}
		if _, done := seen[client]; done {
			continue
		}
		seen[client] = struct{}{}
		if clusterClient, ok := client.(*redis.ClusterClient); ok {
			if err := clusterClient.ForEachMaster(ctx, func(c context.Context, master *redis.Client) error {
				return master.FlushDB(c).Err()
			}); err != nil {
				return err
			}
			continue
		}
		if err := client.FlushDB(ctx).Err(); err != nil {
			return err
		}
	}
	return nil
}

// ============================================================================
// 4. STRUCTURE DE COLLECTION (Le Cœur du DDD)
// ============================================================================

type Collection struct {
	Prefix     string                // ex: "post_service" (donnera "post_service:123")
	Client     redis.UniversalClient // Client Redis universel (Standalone, Sentinel ou Cluster)
	DefaultTTL time.Duration         // Durée de vie par défaut
}

func newCoreCollection(prefix string, ttl time.Duration) *Collection {
	return &Collection{
		Prefix:     prefix,
		Client:     redisgo.CoreClient(),
		DefaultTTL: ttl,
	}
}

func newObjectCollection(prefix string, ttl time.Duration) *Collection {
	return &Collection{
		Prefix:     prefix,
		Client:     redisgo.ObjectClient(),
		DefaultTTL: ttl,
	}
}

func newPubSubCollection(prefix string, ttl time.Duration) *Collection {
	return &Collection{
		Prefix:     prefix,
		Client:     redisgo.PubSubClient(),
		DefaultTTL: ttl,
	}
}

// Key gère la construction stricte de la clé Redis finale : "prefix:id"
func (c *Collection) Key(id any) string {
	return fmt.Sprintf("%s:%v", c.Prefix, id)
}

// Exists vérifie rapidement si la clé d'une collection existe (O(1)).
func (c *Collection) Exists(ctx context.Context, id any) (bool, error) {
	count, err := c.Client.Exists(ctx, c.Key(id)).Result()
	return count > 0, err
}

// ============================================================================
// 5. PRIMITIVES OBJET (JSON / MsgPack)
// ============================================================================

// SetObject stocke une struct Go en MsgPack dans Redis avec le TTL par défaut.
func (c *Collection) SetObject(ctx context.Context, id any, data any) error {
	msgpackBytes, err := msgpack.Marshal(data)
	if err != nil {
		return err
	}
	return c.Client.Set(ctx, c.Key(id), msgpackBytes, c.DefaultTTL).Err()
}

// GetObject récupère un objet et le désérialise dans 'dest'.
func (c *Collection) GetObject(ctx context.Context, id any, dest any) error {
	val, err := c.Client.Get(ctx, c.Key(id)).Bytes()
	if err != nil {
		return err // redis.Nil si absent
	}
	return msgpack.Unmarshal(val, dest)
}

// DeleteObject supprime un objet du cache.
func (c *Collection) DeleteObject(ctx context.Context, id any) error {
	return c.Client.Del(ctx, c.Key(id)).Err()
}

// RefreshTTL prolonge la durée de vie d'un objet (utile pour les sessions).
func (c *Collection) RefreshTTL(ctx context.Context, id any) error {
	return c.Client.Expire(ctx, c.Key(id), c.DefaultTTL).Err()
}

// ============================================================================
// 6. PRIMITIVES BRUTES (Primitives & Entiers)
// ============================================================================

// SetPrimitive stocke une valeur brute sans MsgPack (utile pour les index).
func (c *Collection) SetPrimitive(ctx context.Context, id any, val any) error {
	return c.Client.Set(ctx, c.Key(id), val, c.DefaultTTL).Err()
}

// DeletePrimitive supprime une valeur primitive brute.
func (c *Collection) DeletePrimitive(ctx context.Context, id any) error {
	return c.Client.Del(ctx, c.Key(id)).Err()
}

// GetInt64 récupère une valeur primitive brute sous forme d'entier.
func (c *Collection) GetInt64(ctx context.Context, id any) (int64, error) {
	return c.Client.Get(ctx, c.Key(id)).Int64()
}

// Incr incrémente une valeur numérique et retourne le nouveau compteur.
func (c *Collection) Incr(ctx context.Context, id any) (int64, error) {
	return c.Client.Incr(ctx, c.Key(id)).Result()
}

// SetNX pose une valeur atomiquement uniquement si la clé n'existe pas encore (Idempotence / Anti-Replay).
func (c *Collection) SetNX(ctx context.Context, id any, val any, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		ttl = c.DefaultTTL
	}
	return c.Client.SetNX(ctx, c.Key(id), val, ttl).Result()
}

// ============================================================================
// 7. PRIMITIVES HASH & HYPERLOGLOG
// ============================================================================

func (c *Collection) HGet(ctx context.Context, id any, field string) *redis.StringCmd {
	return c.Client.HGet(ctx, c.Key(id), field)
}

func (c *Collection) HSet(ctx context.Context, id any, values ...any) error {
	return c.Client.HSet(ctx, c.Key(id), values...).Err()
}

func (c *Collection) HGetAll(ctx context.Context, id any) *redis.MapStringStringCmd {
	return c.Client.HGetAll(ctx, c.Key(id))
}

func (c *Collection) HMGet(ctx context.Context, id any, fields ...string) ([]any, error) {
	if len(fields) == 0 {
		return []any{}, nil
	}
	return c.Client.HMGet(ctx, c.Key(id), fields...).Result()
}

func (c *Collection) HIncrBy(ctx context.Context, id any, field string, incr int64) (int64, error) {
	return c.Client.HIncrBy(ctx, c.Key(id), field, incr).Result()
}

func (c *Collection) HDel(ctx context.Context, id any, fields ...string) error {
	return c.Client.HDel(ctx, c.Key(id), fields...).Err()
}

// PFAdd ajoute un ou plusieurs éléments dans un HyperLogLog Redis.
func (c *Collection) PFAdd(ctx context.Context, id any, els ...any) (int64, error) {
	if len(els) == 0 {
		return 0, nil
	}
	return c.Client.PFAdd(ctx, c.Key(id), els...).Result()
}

// PFCount calcule la cardinalité union d'un ou plusieurs HyperLogLog de la collection (Cluster-safe).
func (c *Collection) PFCount(ctx context.Context, ids ...any) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = c.Key(id)
	}
	if redisgo.IsClusterClient(c.Client) && len(keys) > 1 {
		pipe := c.Client.Pipeline()
		cmds := make([]*redis.IntCmd, len(keys))
		for i, k := range keys {
			cmds[i] = pipe.PFCount(ctx, k)
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
			return 0, err
		}
		var total int64
		for _, cmd := range cmds {
			if n, err := cmd.Result(); err == nil {
				total += n
			}
		}
		return total, nil
	}
	return c.Client.PFCount(ctx, keys...).Result()
}

// ExpireNX applique un TTL uniquement si la clé ne possède pas encore d'expiration (Redis 7+ EXPIRE ... NX).
func (c *Collection) ExpireNX(ctx context.Context, id any, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		ttl = c.DefaultTTL
	}
	if ttl <= 0 {
		return false, nil
	}
	seconds := int64(ttl / time.Second)
	if seconds <= 0 {
		seconds = 1
	}
	ok, err := c.Client.Do(ctx, "EXPIRE", c.Key(id), seconds, "NX").Bool()
	if err != nil {
		// Compatibilité descendante si Redis < 7 rejette l'option NX
		return c.Client.Expire(ctx, c.Key(id), ttl).Result()
	}
	return ok, nil
}

// HashReplaceAtomic remplace atomiquement un HASH (DEL + HSET + EXPIRE dans MULTI/EXEC).
func (c *Collection) HashReplaceAtomic(ctx context.Context, id any, fields map[string]any, ttl time.Duration) error {
	key := c.Key(id)
	if ttl <= 0 {
		ttl = c.DefaultTTL
	}
	pipe := c.Client.TxPipeline()
	pipe.Del(ctx, key)
	if len(fields) > 0 {
		args := make([]any, 0, len(fields)*2)
		for k, v := range fields {
			args = append(args, k, v)
		}
		pipe.HSet(ctx, key, args...)
		if ttl > 0 {
			pipe.Expire(ctx, key, ttl)
		}
	}
	_, err := pipe.Exec(ctx)
	return err
}

// ============================================================================
// 8. PRIMITIVES SET & LIST
// ============================================================================

func (c *Collection) SAdd(ctx context.Context, id any, members ...any) error {
	return c.Client.SAdd(ctx, c.Key(id), members...).Err()
}

func (c *Collection) SRem(ctx context.Context, id any, members ...any) error {
	return c.Client.SRem(ctx, c.Key(id), members...).Err()
}

func (c *Collection) SMembers(ctx context.Context, id any) ([]string, error) {
	return c.Client.SMembers(ctx, c.Key(id)).Result()
}

func (c *Collection) SIsMember(ctx context.Context, id any, member any) (bool, error) {
	return c.Client.SIsMember(ctx, c.Key(id), member).Result()
}

func (c *Collection) SCard(ctx context.Context, id any) (int64, error) {
	return c.Client.SCard(ctx, c.Key(id)).Result()
}

func (c *Collection) SAddCount(ctx context.Context, id any, members ...any) (int64, error) {
	return c.Client.SAdd(ctx, c.Key(id), members...).Result()
}

func (c *Collection) SRemCount(ctx context.Context, id any, members ...any) (int64, error) {
	return c.Client.SRem(ctx, c.Key(id), members...).Result()
}

// SInter effectue l'intersection Redis SINTER sur un ou plusieurs identifiants de la collection (Cluster-safe).
func (c *Collection) SInter(ctx context.Context, ids ...any) ([]string, error) {
	if len(ids) == 0 {
		return []string{}, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = c.Key(id)
	}
	if redisgo.IsClusterClient(c.Client) && len(keys) > 1 {
		pipe := c.Client.Pipeline()
		cmds := make([]*redis.StringSliceCmd, len(keys))
		for i, k := range keys {
			cmds[i] = pipe.SMembers(ctx, k)
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
			return nil, err
		}
		first, err := cmds[0].Result()
		if err != nil || len(first) == 0 {
			return []string{}, nil
		}
		common := make(map[string]struct{}, len(first))
		for _, m := range first {
			common[m] = struct{}{}
		}
		for _, cmd := range cmds[1:] {
			members, errM := cmd.Result()
			if errM != nil || len(members) == 0 {
				return []string{}, nil
			}
			next := make(map[string]struct{}, len(common))
			for _, m := range members {
				if _, ok := common[m]; ok {
					next[m] = struct{}{}
				}
			}
			common = next
			if len(common) == 0 {
				return []string{}, nil
			}
		}
		out := make([]string, 0, len(common))
		for m := range common {
			out = append(out, m)
		}
		return out, nil
	}
	return c.Client.SInter(ctx, keys...).Result()
}

func (c *Collection) LPush(ctx context.Context, id any, values ...any) error {
	return c.Client.LPush(ctx, c.Key(id), values...).Err()
}

func (c *Collection) RPush(ctx context.Context, id any, values ...any) error {
	return c.Client.RPush(ctx, c.Key(id), values...).Err()
}

func (c *Collection) LPop(ctx context.Context, id any) (string, error) {
	return c.Client.LPop(ctx, c.Key(id)).Result()
}

func (c *Collection) RPop(ctx context.Context, id any) (string, error) {
	return c.Client.RPop(ctx, c.Key(id)).Result()
}

func (c *Collection) LLen(ctx context.Context, id any) (int64, error) {
	return c.Client.LLen(ctx, c.Key(id)).Result()
}

func (c *Collection) LRange(ctx context.Context, id any, start, stop int64) ([]string, error) {
	return c.Client.LRange(ctx, c.Key(id), start, stop).Result()
}

// ============================================================================
// 9. BULK OPERATIONS & PIPELINE
// ============================================================================

func (c *Collection) Pipeline() redis.Pipeliner {
	return c.Client.Pipeline()
}

// mgetKeysClusterSafe exécute un MGET natif sur Standalone/Sentinel ou un Pipeline de GET en mode Cluster
// (1 RTT parallèle par nœud, zéro CROSSSLOT, ordre exact et nil sur miss préservés).
func mgetKeysClusterSafe(ctx context.Context, client redis.UniversalClient, keys []string) ([]interface{}, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	if !redisgo.IsClusterClient(client) || len(keys) == 1 {
		return client.MGet(ctx, keys...).Result()
	}

	pipe := client.Pipeline()
	cmds := make([]*redis.StringCmd, len(keys))
	for i, k := range keys {
		cmds[i] = pipe.Get(ctx, k)
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	values := make([]interface{}, len(keys))
	for i, cmd := range cmds {
		val, err := cmd.Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				values[i] = nil
				continue
			}
			return nil, err
		}
		values[i] = val
	}
	return values, nil
}

// delKeysClusterSafe exécute DEL natif sur Standalone/Sentinel ou un Pipeline de DEL unitaires en mode Cluster.
func delKeysClusterSafe(ctx context.Context, client redis.UniversalClient, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	if !redisgo.IsClusterClient(client) || len(keys) == 1 {
		return client.Del(ctx, keys...).Err()
	}
	pipe := client.Pipeline()
	for _, k := range keys {
		pipe.Del(ctx, k)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (c *Collection) MGet(ctx context.Context, ids ...any) ([]interface{}, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = c.Key(id)
	}
	return mgetKeysClusterSafe(ctx, c.Client, keys)
}

type GetManyResult struct {
	Found      map[int64][]byte
	MissingIDs []int64
}

// GetMany récupère une liste d'objets en un seul aller-retour (MGET ou Pipeline Cluster-safe).
func (c *Collection) GetMany(ctx context.Context, ids []int64) (*GetManyResult, error) {
	if len(ids) == 0 {
		return &GetManyResult{}, nil
	}

	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = c.Key(id)
	}

	values, err := mgetKeysClusterSafe(ctx, c.Client, keys)
	if err != nil {
		return nil, err
	}

	result := &GetManyResult{
		Found:      make(map[int64][]byte),
		MissingIDs: make([]int64, 0),
	}

	for i, val := range values {
		originalID := ids[i]
		if val == nil {
			result.MissingIDs = append(result.MissingIDs, originalID)
		} else {
			if strVal, ok := val.(string); ok {
				result.Found[originalID] = []byte(strVal)
			} else {
				result.MissingIDs = append(result.MissingIDs, originalID)
			}
		}
	}
	return result, nil
}

// SetMany enregistre un lot d'objets en MsgPack dans Redis via un Pipeline unique (O(1) RTT).
func (c *Collection) SetMany(ctx context.Context, items map[int64]any) error {
	if len(items) == 0 {
		return nil
	}
	pipe := c.Client.Pipeline()
	for id, data := range items {
		msgpackBytes, err := msgpack.Marshal(data)
		if err != nil {
			return err
		}
		pipe.Set(ctx, c.Key(id), msgpackBytes, c.DefaultTTL)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// DeleteMany supprime un lot d'identifiants de la collection (Cluster-safe).
func (c *Collection) DeleteMany(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = c.Key(id)
	}
	return delKeysClusterSafe(ctx, c.Client, keys)
}

// ZSetReplaceAtomic remplace atomiquement le contenu d'un ZSET (DEL + ZADD + EXPIRE via MULTI/EXEC)
// sans jamais exposer de liste partiellement écrite aux lecteurs concurrents.
func (c *Collection) ZSetReplaceAtomic(ctx context.Context, id any, members []*redis.Z, ttl time.Duration) error {
	key := c.Key(id)
	if ttl <= 0 {
		ttl = c.DefaultTTL
	}
	pipe := c.Client.TxPipeline()
	pipe.Del(ctx, key)
	if len(members) > 0 {
		zVals := make([]redis.Z, 0, len(members))
		for _, m := range members {
			if m != nil {
				zVals = append(zVals, *m)
			}
		}
		if len(zVals) > 0 {
			pipe.ZAdd(ctx, key, zVals...)
		}
		if ttl > 0 {
			pipe.Expire(ctx, key, ttl)
		}
	}
	_, err := pipe.Exec(ctx)
	return err
}

// ============================================================================
// 10. CUCKOO FILTER (RedisBloom)
// ============================================================================

func (c *Collection) CFExists(ctx context.Context, id any, item any) (bool, error) {
	return c.Client.Do(ctx, "CF.EXISTS", c.Key(id), item).Bool()
}

func (c *Collection) CFAdd(ctx context.Context, id any, item any) error {
	return c.Client.Do(ctx, "CF.ADD", c.Key(id), item).Err()
}

// CFReserve initialise un Cuckoo Filter dans Redis avec la capacité et les paramètres configurés.
func (c *Collection) CFReserve(ctx context.Context, id any, capacity int) error {
	if capacity <= 0 {
		capacity = variables.UserRecoCuckooCapacity
	}
	key := c.Key(id)
	err := c.Client.Do(
		ctx,
		"CF.RESERVE",
		key,
		capacity,
		"BUCKETSIZE",
		variables.UserRecoCuckooBucketSize,
		"MAXITERATIONS",
		variables.UserRecoCuckooMaxIterations,
		"EXPANSION",
		variables.UserRecoCuckooExpansionFactor,
	).Err()
	if err != nil {
		return err
	}
	if c.DefaultTTL > 0 {
		_ = c.Client.Expire(ctx, key, c.DefaultTTL).Err()
	}
	return nil
}

// CFAddItems insère un lot d'éléments dans un Cuckoo Filter via un Pipeline unique.
func (c *Collection) CFAddItems(ctx context.Context, id any, items []any) error {
	if len(items) == 0 {
		return nil
	}
	key := c.Key(id)
	pipe := c.Client.Pipeline()
	for _, it := range items {
		pipe.Do(ctx, "CF.ADD", key, it)
	}
	if c.DefaultTTL > 0 {
		pipe.Expire(ctx, key, c.DefaultTTL)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// CFExistsMany vérifie en un seul aller-retour (Pipeline de CF.EXISTS) quels candidats sont présents dans le Cuckoo Filter.
func (c *Collection) CFExistsMany(ctx context.Context, id any, items []int64) (map[int64]bool, error) {
	res := make(map[int64]bool, len(items))
	if len(items) == 0 {
		return res, nil
	}
	key := c.Key(id)
	pipe := c.Client.Pipeline()
	cmds := make([]*redis.Cmd, len(items))
	for i, item := range items {
		cmds[i] = pipe.Do(ctx, "CF.EXISTS", key, item)
	}
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	for i, cmd := range cmds {
		exists, errBool := cmd.Bool()
		if errBool == nil && exists {
			res[items[i]] = true
		}
	}
	return res, nil
}

// ============================================================================
// 11. PUB/SUB FLUX (Pattern Claim Check - DDD, Sharded Pub/Sub en Cluster)
// ============================================================================

// PushFlux publie un message lourd (stockage RAM puis notification).
func (c *Collection) PushFlux(ctx context.Context, message []byte) error {
	messageID := strconv.FormatInt(domain.NowTime().UnixNano(), 10)

	payloadKey := c.Key("msg:" + messageID)
	if err := c.Client.Set(ctx, payloadKey, message, c.DefaultTTL).Err(); err != nil {
		return err
	}

	channelKey := c.Key("channel")
	if redisgo.IsClusterClient(c.Client) {
		return c.Client.SPublish(ctx, channelKey, messageID).Err()
	}
	return c.Client.Publish(ctx, channelKey, messageID).Err()
}

// SubscribeFlux s'abonne au canal et récupère automatiquement les payloads (Claim Check).
func (c *Collection) SubscribeFlux(ctx context.Context) (<-chan []byte, context.CancelFunc) {
	channelKey := c.Key("channel")

	subCtx, cancel := context.WithCancel(ctx)
	var pubsub *redis.PubSub
	if redisgo.IsClusterClient(c.Client) {
		pubsub = c.Client.SSubscribe(subCtx, channelKey)
	} else {
		pubsub = c.Client.Subscribe(subCtx, channelKey)
	}
	ch := make(chan []byte, 100)

	go func() {
		defer func() {
			if err := pubsub.Close(); err != nil {
				numan_log.Error(ctx).Err(err).Msg("Erreur fermeture pubsub")
			}
		}()
		defer close(ch)

		for msg := range pubsub.Channel() {
			messageID := msg.Payload
			payloadKey := c.Key("msg:" + messageID)

			readCtx, readCancel := context.WithTimeout(subCtx, 2*time.Second)
			data, err := c.Client.Get(readCtx, payloadKey).Bytes()
			readCancel()

			if err != nil {
				continue
			}

			select {
			case ch <- data:
			case <-subCtx.Done():
				return
			}
		}
	}()

	return ch, cancel
}

// ============================================================================
// 12. PUB/SUB (Mode DDD strict — Sharded Pub/Sub O(1) en Cluster)
// ============================================================================

// Publish publie un message sur un canal Pub/Sub (SPUBLISH en mode Redis Cluster 7).
func (c *Collection) Publish(ctx context.Context, id any, message any) error {
	if redisgo.IsClusterClient(c.Client) {
		return c.Client.SPublish(ctx, c.Key(id), message).Err()
	}
	return c.Client.Publish(ctx, c.Key(id), message).Err()
}

// PublishMultiple publie un même message sur plusieurs canaux via un Pipeline (O(1) RTT).
// Utilise SPUBLISH en mode Redis Cluster 7 pour éviter tout broadcast inter-nœuds.
func (c *Collection) PublishMultiple(ctx context.Context, ids []int64, message any) error {
	if len(ids) == 0 {
		return nil
	}
	isCluster := redisgo.IsClusterClient(c.Client)
	pipe := c.Client.Pipeline()
	for _, id := range ids {
		if isCluster {
			pipe.SPublish(ctx, c.Key(id), message)
		} else {
			pipe.Publish(ctx, c.Key(id), message)
		}
	}
	_, err := pipe.Exec(ctx)
	return err
}

// ============================================================================
// 13. CONFIGURATION DYNAMIQUE ATOMIQUE (LUA SCRIPTS - DDD)
// ============================================================================

const applyVariablesPatchScript = `
local overrides_key = KEYS[1]
local version_key = KEYS[2]
local set_count = tonumber(ARGV[1])
local idx = 2

for i = 1, set_count do
    local k = ARGV[idx]
    local v = ARGV[idx + 1]
    redis.call('HSET', overrides_key, k, v)
    idx = idx + 2
end

local del_count = tonumber(ARGV[idx])
idx = idx + 1
for i = 1, del_count do
    local k = ARGV[idx]
    redis.call('HDEL', overrides_key, k)
    idx = idx + 1
end

local new_version = redis.call('INCR', version_key)
return new_version
`

const loadVariablesStateScript = `
local overrides_key = KEYS[1]
local version_key = KEYS[2]
local ver = tonumber(redis.call('GET', version_key) or '0')
local raw = redis.call('HGETALL', overrides_key)
return {ver, raw}
`

// ApplyVariablesPatchAtomic applique un lot de mises à jour/suppressions dans le Hash Redis
// et incrémente le numéro de version global en une seule transaction atomique Lua.
func (c *Collection) ApplyVariablesPatchAtomic(
	ctx context.Context,
	overridesID any,
	versionID any,
	toSet map[string]string,
	toDelete []string,
) (int64, error) {
	if c == nil || c.Client == nil {
		return 0, numan_error.NewInternal(errors.New("collection Redis non initialisée"))
	}

	args := make([]any, 0, 2+len(toSet)*2+len(toDelete))
	args = append(args, len(toSet))
	for k, v := range toSet {
		args = append(args, k, v)
	}
	args = append(args, len(toDelete))
	for _, k := range toDelete {
		args = append(args, k)
	}

	keys := []string{c.Key(overridesID), c.Key(versionID)}
	return c.Client.Eval(ctx, applyVariablesPatchScript, keys, args...).Int64()
}

// LoadVariablesStateAtomic récupère de manière atomique le couple (version, overrides) en 1 seul RTT.
func (c *Collection) LoadVariablesStateAtomic(
	ctx context.Context,
	overridesID any,
	versionID any,
) (map[string]string, int64, error) {
	if c == nil || c.Client == nil {
		return nil, 0, numan_error.NewInternal(errors.New("collection Redis non initialisée"))
	}

	keys := []string{c.Key(overridesID), c.Key(versionID)}
	res, err := c.Client.Eval(ctx, loadVariablesStateScript, keys).Result()
	if err != nil {
		return nil, 0, err
	}

	slice, ok := res.([]any)
	if !ok || len(slice) != 2 {
		return nil, 0, numan_error.NewInternal(errors.New("réponse Lua inattendue pour LoadVariablesStateAtomic"))
	}

	version, _ := slice[0].(int64)
	overrides := make(map[string]string)

	if rawList, ok := slice[1].([]any); ok {
		for i := 0; i+1 < len(rawList); i += 2 {
			k, kOk := rawList[i].(string)
			v, vOk := rawList[i+1].(string)
			if kOk && vOk {
				overrides[k] = v
			}
		}
	}

	return overrides, version, nil
}

// RefreshStandardTTL met à jour dynamiquement le DefaultTTL de toutes les collections
// indexées sur StandardTTL lorsqu'un administrateur modifie StandardTTL à chaud.
func RefreshStandardTTL(newTTL time.Duration) {
	collections := []*Collection{
		Users, UserSettings, Sessions, Posts, Comments, Media, Messages,
		Conversations, Members, Relations, Saved, PostComments, UserTimeline,
		NotificationsObj, ConvMeta, ConvMembers, SpeedAddable, SpeedCommunity,
		FeedsObject, FeedsMailbox, FeedsPersonalized, ContentVectors, RankedPosts,
		TagPosts, SessionIndexes, CuckooSeen, LSHBuckets, ConvWatermarks, Passkeys,
		MessageReactionCounts, MessageUserReactions, NotificationsZSet, TelemetryProfiles,
		POIReviews, POIReviewsByPOI, POIReviewUserIndex,
	}
	for _, col := range collections {
		if col != nil {
			col.DefaultTTL = newTTL
		}
	}
}

// Expire applique un TTL explicite sur une clé de la collection.
func (c *Collection) Expire(ctx context.Context, id any, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = c.DefaultTTL
	}
	if ttl <= 0 {
		return nil
	}
	return c.Client.Expire(ctx, c.Key(id), ttl).Err()
}

// SetRawWithTTL stocke une valeur brute (JSON/string) avec un TTL explicite.
func (c *Collection) SetRawWithTTL(ctx context.Context, id any, val any, ttl time.Duration) error {
	if c == nil || c.Client == nil {
		return errors.New("collection Redis non initialisée")
	}
	return c.Client.Set(ctx, c.Key(id), val, ttl).Err()
}

// GetBytes récupère la valeur brute d'une clé de la collection.
func (c *Collection) GetBytes(ctx context.Context, id any) ([]byte, error) {
	if c == nil || c.Client == nil {
		return nil, errors.New("collection Redis non initialisée")
	}
	return c.Client.Get(ctx, c.Key(id)).Bytes()
}

const releaseLockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
else
    return 0
end
`

var releaseLockCompiled = redis.NewScript(releaseLockScript)

// AcquireCronLock acquiert un verrou distribué atomique (SETNX) pour un worker Cron et retourne un jeton unique.
func AcquireCronLock(ctx context.Context, jobName string, ttl time.Duration) (string, bool, error) {
	if CronLocks == nil || CronLocks.Client == nil {
		return "", false, nil
	}
	token := fmt.Sprintf("%d", domain.NowTime().UnixNano())
	acquired, err := CronLocks.SetNX(ctx, jobName, token, ttl)
	if err != nil || !acquired {
		return "", false, err
	}
	return token, true, nil
}

// ReleaseCronLock libère un verrou distribué Cron uniquement si le jeton correspond à l'instance détentrice.
func ReleaseCronLock(ctx context.Context, jobName, token string) error {
	if CronLocks == nil || CronLocks.Client == nil || token == "" {
		return nil
	}
	return releaseLockCompiled.Run(ctx, CronLocks.Client, []string{CronLocks.Key(jobName)}, token).Err()
}

// ScanKeys itère de manière non-bloquante via SCAN sur un motif de clés Redis (Cluster-safe via ForEachMaster).
func ScanKeys(ctx context.Context, pattern string, count int64) ([]string, error) {
	return ScanKeysOnClient(ctx, redisgo.CoreClient(), pattern, count)
}

// ScanKeysOnClient itère de manière non-bloquante via SCAN sur un client Redis spécifique (Cluster-safe).
func ScanKeysOnClient(ctx context.Context, client redis.UniversalClient, pattern string, count int64) ([]string, error) {
	if client == nil {
		return nil, nil
	}
	if count <= 0 {
		count = 200
	}
	if clusterClient, ok := client.(*redis.ClusterClient); ok {
		var all []string
		err := clusterClient.ForEachMaster(ctx, func(c context.Context, master *redis.Client) error {
			var cursor uint64
			for {
				keys, nextCursor, errS := master.Scan(c, cursor, pattern, count).Result()
				if errS != nil {
					return errS
				}
				all = append(all, keys...)
				cursor = nextCursor
				if cursor == 0 {
					break
				}
			}
			return nil
		})
		return all, err
	}

	var (
		cursor uint64
		all    []string
	)
	for {
		keys, nextCursor, err := client.Scan(ctx, cursor, pattern, count).Result()
		if err != nil {
			return all, err
		}
		all = append(all, keys...)
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return all, nil
}
