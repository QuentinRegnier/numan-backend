package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/go-redis/redis/v8"
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
	ContentVectors     *Collection
	RankedPosts        *Collection // Les ZSETs stricts (likes, views, recent)
	TagPosts           *Collection // Les ZSETs par tags pour l'UI
	TrendGlobalHourly  *Collection
	TrendGlobalDaily   *Collection
	TrendTagDaily      *Collection
	TrendTagWeekly     *Collection
	HashtagLeaderboard *Collection

	// --- SYSTEM Cache ---
	RateLimits   *Collection
	DLQ          *Collection
	GraphEdges   *Collection
	Tags         *Collection
	HashtagCanon *Collection

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
)

// ============================================================================
// 2. INITIALISATION
// ============================================================================

func InitCacheDatabase() {
	// --- OBJECT Cache ---
	Users = newCollection("object_cache:user", variables.StandardTTL)
	UserSettings = newCollection("object_cache:user_settings", variables.StandardTTL)
	Sessions = newCollection("object_cache:session", variables.StandardTTL)
	Passkeys = newCollection("object_cache:passkey", variables.StandardTTL)
	Posts = newCollection("object_cache:post_service", variables.StandardTTL)
	Comments = newCollection("object_cache:comment", variables.StandardTTL)
	Media = newCollection("object_cache:media", variables.StandardTTL)
	Messages = newCollection("object_cache:msg", variables.StandardTTL)
	Conversations = newCollection("object_cache:conv", variables.StandardTTL)
	Members = newCollection("object_cache:member", variables.StandardTTL)
	Relations = newCollection("object_cache:rel", variables.StandardTTL)
	Saved = newCollection("object_cache:saved:zset", variables.StandardTTL)
	PostComments = newCollection("object:comments:zset", variables.StandardTTL)
	UserTimeline = newCollection("user_cache:posts:zset", variables.StandardTTL)
	NotificationsObj = newCollection("object_cache:notification", variables.StandardTTL)

	// --- SPEED Cache ---
	UsersLite = newCollection("speed_cache:user:lite", 0)
	UsersLex = newCollection("speed_cache:user:search", 0)
	CommunitiesLex = newCollection("speed_cache:community:search", 0)
	TagsLex = newCollection("speed_cache:tag:search", 0)
	ConvMeta = newCollection("speed_cache:conversation:meta", variables.StandardTTL)
	ConvMembers = newCollection("speed_cache:conversation:members", variables.StandardTTL)
	SpeedRelationsIndex = newCollection("speed_cache:relations:index:default", 0)
	SpeedRelations = newCollection("speed_cache:relations:default", 0)
	SpeedAddable = newCollection("speed_cache:addable:default", variables.StandardTTL)
	SpeedCommunity = newCollection("speed_cache:community:default", variables.StandardTTL)

	// --- FEED Cache ---
	FeedsObject = newCollection("feed:state", variables.StandardTTL)
	FeedsMailbox = newCollection("feed_cache:mailbox", variables.StandardTTL)
	FeedsPersonalized = newCollection("feed:personalized", variables.StandardTTL)

	// --- ALGORITHM & MOST Cache ---
	ContentVectors = newCollection("most_cache:vec", variables.StandardTTL)
	RankedPosts = newCollection("most_cache", variables.StandardTTL)
	TagPosts = newCollection("most_cache:idx:tag", variables.StandardTTL)
	TrendGlobalHourly = newCollection("trend:global:hourly", 0)
	TrendGlobalDaily = newCollection("trend:global:daily", 0)
	TrendTagDaily = newCollection("trend:tag:daily", 0)
	TrendTagWeekly = newCollection("trend:tag:weekly", 0)
	HashtagLeaderboard = newCollection("hashtag:leaderboard", 0)

	// --- SYSTEM Cache ---
	RateLimits = newCollection("rate_limit:ip", 10*time.Second)
	DLQ = newCollection("dlq", 0)
	GraphEdges = newCollection("graph_cache:tag_edges", 0)
	Tags = newCollection("tags", 0)
	HashtagCanon = newCollection("hashtag:canon", 0)

	// --- INDEX & IDEMPOTENCE ---
	SessionIndexes = newCollection("session_cache", variables.StandardTTL)
	SessionBlacklist = newCollection("blacklist:session", 24*time.Hour)
	PostLikesSet = newCollection("post:likes_set", 0)
	CommentLikesSet = newCollection("comment:likes_set", 0)
	SystemStatus = newCollection("system:status", 0)

	// --- CUCKOO FILTER ---
	CuckooSeen = newCollection("cuckoo:seen", variables.StandardTTL)
	CuckooSync = newCollection("cuckoo:sync", 5*time.Second) // NOUVEAU : TTL court encapsulé !

	// --- MESSAGING & LSH ---
	ConvParticipants = newCollection("conv:participants", 0)
	UserInbox = newCollection("inbox:user", 0)
	MessagesIndex = newCollection("messages:idx", 24*time.Hour)
	LSHBuckets = newCollection("lsh:bucket", variables.StandardTTL)
	ConvWatermarks = newCollection("conv:watermarks", variables.StandardTTL)

	UserSyncLedger = newCollection("sync:ledger:user", 30*24*time.Hour)
	ConvMessageLedger = newCollection("sync:ledger:conv_msg", 30*24*time.Hour)

	// --- RÉACTIONS AUX MESSAGES ---
	MessageReactionCounts = newCollection("msg_react_counts", variables.StandardTTL)
	MessageUserReactions = newCollection("msg_user_react", variables.StandardTTL)

	// --- ACTIVITY FEED ---
	FeedSchedule = newCollection("feed:precompute:schedule", 0)
	NotificationsZSet = newCollection("notifications:user", variables.StandardTTL)
	NotificationCursors = newCollection("notifications:cursor", 0)
	NotificationActivity = newCollection("notifications:activity", 0)
	InboxActivity = newCollection("inbox:activity", 0)

	// --- TELEMETRY Cache ---
	TelemetryProfiles = newCollection("telemetry:profiles", variables.StandardTTL)

	// --- WEBSOCKET Cache ---
	Presence = newCollection("presence:user", 60*time.Second)

	// --- PUB/SUB CHANNELS & WORKER QUEUES ---
	ChannelUser = newCollection("channel:user", 0)
	ChannelCommunity = newCollection("channel:community", 0)
	WorkerQueue = newCollection("worker:queue", 0)

	// --- CONFIGURATION DYNAMIQUE CENTRALISÉE ---
	DynamicVariables = newCollection("system:variables", 0)
	DynamicVariablesSync = newCollection("system:variables:sync", 10*time.Second)

	// --- PASSKEY TWO FACTOR ---
	PreAuth = newCollection("auth:pre_auth", variables.TwoFactorPreAuthTTL)

	// --- TRUST SCORE & MODERATION ---
	ReportAggregators = newCollection("moderation:report_agg", variables.StandardTTL)
	SteganoBanlist = newCollection("moderation:stegano_banlist", 0)
	ModerationQueues = newCollection("moderation:queue", 0)
	ModerationExpirations = newCollection("moderation:expirations", 0)
	ModerationTickets = newCollection("moderation:ticket", 0)
	ModeratorStats = newCollection("moderation:mod_stats", 0)
}

// ============================================================================
// 3. INFRASTRUCTURE & CYCLE DE VIE
// ============================================================================

// IsReady isole l'état de l'infrastructure pour les routines de maintenance.
func IsReady() bool {
	return redisgo.Rdb != nil
}

// Keys renvoie toutes les clés correspondant à un pattern global.
func Keys(ctx context.Context, pattern string) ([]string, error) {
	return redisgo.Rdb.Keys(ctx, pattern).Result()
}

// FlushDB vide l'intégralité de la base de données Redis courante.
func FlushDB(ctx context.Context) error {
	return redisgo.Rdb.FlushDB(ctx).Err()
}

// ============================================================================
// 4. STRUCTURE DE COLLECTION (Le Cœur du DDD)
// ============================================================================

type Collection struct {
	Prefix     string        // ex: "post_service" (donnera "post_service:123")
	Client     *redis.Client // Client Redis
	DefaultTTL time.Duration // Durée de vie par défaut
}

func newCollection(prefix string, ttl time.Duration) *Collection {
	return &Collection{
		Prefix:     prefix,
		Client:     redisgo.Rdb,
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
// 7. PRIMITIVES HASH
// ============================================================================

func (c *Collection) HGet(ctx context.Context, id any, field string) *redis.StringCmd {
	return c.Client.HGet(ctx, c.Key(id), field)
}

func (c *Collection) HSet(ctx context.Context, id any, values ...any) error {
	return c.Client.HSet(ctx, c.Key(id), values...).Err()
}

func (c *Collection) HGetAll(ctx context.Context, id any) *redis.StringStringMapCmd {
	return c.Client.HGetAll(ctx, c.Key(id))
}

func (c *Collection) HDel(ctx context.Context, id any, fields ...string) error {
	return c.Client.HDel(ctx, c.Key(id), fields...).Err()
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

func (c *Collection) MGet(ctx context.Context, ids ...any) ([]interface{}, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = c.Key(id)
	}
	return c.Client.MGet(ctx, keys...).Result()
}

type GetManyResult struct {
	Found      map[int64][]byte
	MissingIDs []int64
}

// GetMany récupère une liste d'objets en un seul appel réseau (MGET).
func (c *Collection) GetMany(ctx context.Context, ids []int64) (*GetManyResult, error) {
	if len(ids) == 0 {
		return &GetManyResult{}, nil
	}

	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = c.Key(id)
	}

	values, err := c.Client.MGet(ctx, keys...).Result()
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

// ============================================================================
// 10. CUCKOO FILTER (RedisBloom)
// ============================================================================

func (c *Collection) CFExists(ctx context.Context, id any, item any) (bool, error) {
	return c.Client.Do(ctx, "CF.EXISTS", c.Key(id), item).Bool()
}

func (c *Collection) CFAdd(ctx context.Context, id any, item any) error {
	return c.Client.Do(ctx, "CF.ADD", c.Key(id), item).Err()
}

// ============================================================================
// 11. PUB/SUB FLUX (Pattern Claim Check - DDD)
// ============================================================================

// PushFlux publie un message lourd (stockage RAM puis notification)
func (c *Collection) PushFlux(ctx context.Context, message []byte) error {
	// 1. La génération de l'ID est encapsulée ici (zéro fuite d'infrastructure)
	messageID := strconv.FormatInt(time.Now().UnixNano(), 10)

	// 2. Stockage du payload avec le TTL par défaut de la collection
	payloadKey := c.Key("msg:" + messageID)
	if err := c.Client.Set(ctx, payloadKey, message, c.DefaultTTL).Err(); err != nil {
		return err
	}

	// 3. Publication de l'ID sur le canal de cette collection
	channelKey := c.Key("channel")
	return c.Client.Publish(ctx, channelKey, messageID).Err()
}

// SubscribeFlux s'abonne au canal et récupère automatiquement les payloads (Claim Check)
func (c *Collection) SubscribeFlux(ctx context.Context) (<-chan []byte, context.CancelFunc) {
	channelKey := c.Key("channel")

	// Sous-contexte pour pouvoir annuler l'écoute proprement
	subCtx, cancel := context.WithCancel(ctx)
	pubsub := c.Client.Subscribe(subCtx, channelKey)
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

			// Récupération du payload lourd stocké juste avant la publication
			readCtx, readCancel := context.WithTimeout(subCtx, 2*time.Second)
			data, err := c.Client.Get(readCtx, payloadKey).Bytes()
			readCancel()

			if err != nil {
				// Si absent (TTL expiré ou erreur), on l'ignore silencieusement
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
// 12. PUB/SUB (Mode DDD strict)
// ============================================================================

// Publish publie un message sur un canal Pub/Sub
func (c *Collection) Publish(ctx context.Context, id any, message any) error {
	return c.Client.Publish(ctx, c.Key(id), message).Err()
}

// PublishMultiple publie un même message sur plusieurs canaux via un Pipeline (O(1) RTT)
func (c *Collection) PublishMultiple(ctx context.Context, ids []int64, message any) error {
	if len(ids) == 0 {
		return nil
	}
	pipe := c.Client.Pipeline()
	for _, id := range ids {
		pipe.Publish(ctx, c.Key(id), message)
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
	}
	for _, col := range collections {
		if col != nil {
			col.DefaultTTL = newTTL
		}
	}
}
