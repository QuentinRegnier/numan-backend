package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/redis/go-redis/v9"
)

// --- CONSTANTES ---

type ActionType string

const (
	ActionCreate                      ActionType = "ADD"
	ActionUpdate                      ActionType = "UPD"
	ActionDelete                      ActionType = "DEL"
	ActionBuild                       ActionType = "BUILD"
	ActionGenerateUserRecommendations ActionType = "GENERATE_USER_RECOMMENDATIONS"
)

// EntityType : Sur quoi on agit
type EntityType string

const (
	EntityUser               EntityType = "Users"
	EntityUserSettings       EntityType = "UserSettings"
	EntitySession            EntityType = "Sessions"
	EntityRelation           EntityType = "Relations"
	EntityPost               EntityType = "Posts"
	EntityComment            EntityType = "Comments"
	EntityLike               EntityType = "Likes"
	EntityMedia              EntityType = "Media"
	EntityConversation       EntityType = "Conversations"
	EntityMembers            EntityType = "Members"
	EntityMessage            EntityType = "Messages"
	EntityMessageReaction    EntityType = "MessageReactions"
	EntityView               EntityType = "Views"
	EntityFeed               EntityType = "Feeds"
	EntityReport             EntityType = "Reports"
	EntityTelemetry          EntityType = "Telemetry"
	EntitySaved              EntityType = "Saved"
	EntityPasskey            EntityType = "Passkeys"
	EntityNotification       EntityType = "Notifications"
	EntityPOI                EntityType = "POIs"
	EntityPOIReview          EntityType = "POIReviews"
	EntityPOICounter         EntityType = "POICounters"
	EntityUserRecoSkip       EntityType = "UserRecoSkips"
	EntityUserRecommendation EntityType = "UserRecommendations"
	EntityArticle            EntityType = "Articles"
)

// DBTarget : Bitmask pour savoir où envoyer (Mongo, Postgres, ou les deux)
type DBTarget int

const (
	TargetMongo    DBTarget = 1 << 0
	TargetPostgres DBTarget = 1 << 1
	TargetWorker   DBTarget = 1 << 2
	TargetAll               = TargetMongo | TargetPostgres
)

// --- STRUCTURE DE L'EVENT ---

type AsyncEvent struct {
	ID        int64      `json:"id"`
	Type      EntityType `json:"t"`
	Action    ActionType `json:"a"`
	Payload   any        `json:"p"`
	Targets   DBTarget   `json:"tg"`
	Timestamp int64      `json:"ts"` // Timestamp UnixMilli
}

// --- QUEUE MANAGER ---

const (
	QueueShards = 64

	// queueBasePrefix et statsBasePrefix utilisent le hash tag {<shardID>}
	// afin que request_cache:q:{shard} et request_cache:stats:{shard} atterrissent
	// systématiquement sur le même slot Redis Cluster.
	queueBasePrefix = "request_cache:q:"
	statsBasePrefix = "request_cache:stats:"
)

// QueueShardKey retourne la clé hash-taggée Redis Cluster pour un shard (ex: request_cache:q:{14}).
func QueueShardKey(shardID int) string {
	return fmt.Sprintf("%s{%d}", queueBasePrefix, shardID)
}

// LegacyQueueShardKey retourne l'ancienne clé non hash-taggée (rolling upgrade drain fallback).
func LegacyQueueShardKey(shardID int) string {
	return fmt.Sprintf("%s%d", queueBasePrefix, shardID)
}

// StatsShardKey retourne la clé hash-taggée des statistiques d'un shard (ex: request_cache:stats:{14}).
func StatsShardKey(shardID int) string {
	return fmt.Sprintf("%s{%d}", statsBasePrefix, shardID)
}

func extractShardStatsKey(queueKey string) string {
	suffix := strings.TrimPrefix(queueKey, queueBasePrefix)
	return statsBasePrefix + suffix
}

// EnqueueDB : Ajout du paramètre partitionKey (int64)
// Si partitionKey est 0, on utilise l'ID de l'objet pour choisir la file.
// Si partitionKey est fourni (ex: ID du User), on utilise ça pour forcer la file.
func EnqueueDB(ctx context.Context, id int64, partitionKey int64, entity EntityType, action ActionType, data interface{}, target DBTarget) error {
	client := redisgo.CoreClient()
	if client == nil {
		return numan_error.NewInternal(errors.New("redis core client not initialized"))
	}

	now := domain.NowMillis()
	event := AsyncEvent{
		ID:        id,
		Type:      entity,
		Action:    action,
		Payload:   data,
		Targets:   target,
		Timestamp: now,
	}

	bytes, err := json.Marshal(event)
	if err != nil {
		return numan_error.NewInternal(err)
	}

	keyForSharding := id
	if partitionKey != 0 {
		keyForSharding = partitionKey
	}
	shardID := int(getShardID(keyForSharding))

	queueKey := QueueShardKey(shardID)
	statsKey := StatsShardKey(shardID)

	pipe := client.Pipeline()
	pipe.RPush(ctx, queueKey, bytes)
	pipe.HIncrBy(ctx, statsKey, "count", 1)
	pipe.HSetNX(ctx, statsKey, "ts", now)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return numan_error.NewInternal(err)
	}

	return nil
}

// --- OUTILS POUR LE WORKER INTELLIGENT ---

// PopSmartBatchBlocking attend une donnée sans consommer de CPU, puis rafle jusqu'à batchSize éléments sur un shard.
func PopSmartBatchBlocking(ctx context.Context, shardID int, batchSize int64) ([]AsyncEvent, error) {
	return PopSmartBatchBlockingMulti(ctx, []int{shardID}, batchSize)
}

// PopSmartBatchBlockingMulti attend des événements sur un ou plusieurs shards assignés à une goroutine worker,
// puis rafle jusqu'à batchSize éléments sur le shard réveillé.
//   - En mode Standalone/Sentinel ou mono-shard : utilise BLPOP bloquant natif (0 CPU).
//   - En mode Redis Cluster multi-shards (>1) : utilise un pipeline LPop multi-shards avec backoff adaptatif
//     (50ms..500ms) pour éviter toute erreur CROSSSLOT sur BLPOP multi-clés.
func PopSmartBatchBlockingMulti(ctx context.Context, shardIDs []int, batchSize int64) ([]AsyncEvent, error) {
	if len(shardIDs) == 0 {
		return nil, nil
	}
	client := redisgo.CoreClient()
	if client == nil {
		return nil, errors.New("redis core client not initialized")
	}

	isCluster := redisgo.IsClusterClient(client)
	if isCluster && len(shardIDs) > 1 {
		return popClusterMultiShardAdaptive(ctx, client, shardIDs, batchSize)
	}

	// Mono-shard en Cluster ou Standalone/Sentinel (1..N shards)
	var queueKeys []string
	if isCluster {
		// En Cluster avec 1 shard : 1 seule clé (hash-taggée) pour BLPOP sans CROSSSLOT.
		// Fallback drain opportuniste de l'ancienne clé legacy avant de bloquer.
		legacyKey := LegacyQueueShardKey(shardIDs[0])
		if rawLegacy, errL := client.LPop(ctx, legacyKey).Result(); errL == nil && rawLegacy != "" {
			return drainRemainingAndDecode(ctx, client, legacyKey, rawLegacy, batchSize), nil
		}
		queueKeys = []string{QueueShardKey(shardIDs[0])}
	} else {
		queueKeys = make([]string, 0, len(shardIDs)*2)
		for _, id := range shardIDs {
			queueKeys = append(queueKeys, QueueShardKey(id), LegacyQueueShardKey(id))
		}
	}

	blpopRes, err := client.BLPop(ctx, 2*time.Second, queueKeys...).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, err
	}

	return drainRemainingAndDecode(ctx, client, blpopRes[0], blpopRes[1], batchSize), nil
}

// popClusterMultiShardAdaptive dépile en mode Redis Cluster multi-shards sans jamais déclencher CROSSSLOT.
func popClusterMultiShardAdaptive(ctx context.Context, client redis.UniversalClient, shardIDs []int, batchSize int64) ([]AsyncEvent, error) {
	deadline := time.Now().Add(2 * time.Second)
	backoff := 50 * time.Millisecond
	const maxClusterBackoff = 500 * time.Millisecond

	keys := make([]string, 0, len(shardIDs)*2)
	for _, id := range shardIDs {
		keys = append(keys, QueueShardKey(id), LegacyQueueShardKey(id))
	}

	for {
		pipe := client.Pipeline()
		cmds := make([]*redis.StringCmd, len(keys))
		for i, k := range keys {
			cmds[i] = pipe.LPop(ctx, k)
		}
		_, err := pipe.Exec(ctx)
		if err != nil && !errors.Is(err, redis.Nil) {
			return nil, err
		}

		var collected []AsyncEvent
		for i, cmd := range cmds {
			val, errCmd := cmd.Result()
			if errCmd != nil || val == "" {
				continue
			}
			remainingBudget := batchSize - int64(len(collected))
			if remainingBudget < 1 {
				remainingBudget = 1
			}
			batch := drainRemainingAndDecode(ctx, client, keys[i], val, remainingBudget)
			collected = append(collected, batch...)
			if int64(len(collected)) >= batchSize {
				break
			}
		}
		if len(collected) > 0 {
			return collected, nil
		}

		if time.Now().After(deadline) {
			return nil, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxClusterBackoff {
			backoff = maxClusterBackoff
		}
	}
}

func drainRemainingAndDecode(ctx context.Context, client redis.UniversalClient, queueKey, firstVal string, batchSize int64) []AsyncEvent {
	statsKey := extractShardStatsKey(queueKey)
	rawElements := []string{firstVal}

	if batchSize > 1 {
		rest, err := client.LPopCount(ctx, queueKey, int(batchSize)-1).Result()
		if err == nil && len(rest) > 0 {
			rawElements = append(rawElements, rest...)
		} else if err != nil && !errors.Is(err, redis.Nil) {
			numan_log.Warn(ctx).Err(err).Str("queue_key", queueKey).Msg("Erreur LPopCount secondaire")
		}
	}

	countPopped := int64(len(rawElements))
	events := make([]AsyncEvent, 0, countPopped)
	for _, res := range rawElements {
		var evt AsyncEvent
		decoder := json.NewDecoder(strings.NewReader(res))
		decoder.UseNumber()

		if err := decoder.Decode(&evt); err == nil {
			events = append(events, evt)
		} else {
			numan_log.Error(ctx).Err(err).Msg("Erreur de décodage d'un événement de la queue asynchrone")
		}
	}

	go updateDashboardAfterPop(context.Background(), client, statsKey, queueKey, countPopped)
	return events
}

// updateDashboardAfterPop gère la cohérence du tableau de bord (Async pour perf)
func updateDashboardAfterPop(ctx context.Context, client redis.UniversalClient, statsKey, queueKey string, poppedCnt int64) {
	if client == nil {
		return
	}
	pipe := client.Pipeline()
	pipe.HIncrBy(ctx, statsKey, "count", -poppedCnt)
	lindexCmd := pipe.LIndex(ctx, queueKey, 0)

	if _, err := pipe.Exec(ctx); err != nil {
		return
	}

	nextHeadVal, err := lindexCmd.Result()
	if err == nil && nextHeadVal != "" {
		var nextEvt AsyncEvent
		if json.Unmarshal([]byte(nextHeadVal), &nextEvt) == nil {
			client.HSet(ctx, statsKey, "ts", nextEvt.Timestamp)
		}
	} else {
		client.HDel(ctx, statsKey, "ts")
		client.HSet(ctx, statsKey, "count", 0)
	}
}

// GetShardID calcule de manière déterministe (CRC32) le shard de file d'attente associé à un identifiant.
func GetShardID(id int64) int {
	return int(getShardID(id))
}

func getShardID(id int64) uint32 {
	idStr := strconv.FormatInt(id, 10)
	return crc32.ChecksumIEEE([]byte(idStr)) % QueueShards
}

// TotalQueueDepth retourne le nombre total d'événements Write-Behind en attente sur les 64 shards Redis (Cluster-safe).
func TotalQueueDepth(ctx context.Context) (int64, error) {
	client := redisgo.CoreClient()
	if client == nil {
		return 0, errors.New("redis client not initialized")
	}
	pipe := client.Pipeline()
	cmds := make([]*redis.IntCmd, 0, QueueShards*2)
	for i := 0; i < QueueShards; i++ {
		cmds = append(cmds, pipe.LLen(ctx, QueueShardKey(i)), pipe.LLen(ctx, LegacyQueueShardKey(i)))
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

// OldestQueueEventAgeSeconds calcule l'âge en secondes de l'événement Write-Behind le plus ancien sur les 64 shards (Cluster-safe).
func OldestQueueEventAgeSeconds(ctx context.Context) (float64, error) {
	client := redisgo.CoreClient()
	if client == nil {
		return 0, errors.New("redis client not initialized")
	}
	pipe := client.Pipeline()
	cmds := make([]*redis.StringCmd, 0, QueueShards*2)
	for i := 0; i < QueueShards; i++ {
		cmds = append(cmds, pipe.HGet(ctx, StatsShardKey(i), "ts"), pipe.HGet(ctx, fmt.Sprintf("%s%d", statsBasePrefix, i), "ts"))
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return 0, err
	}

	nowMs := domain.NowMillis()
	var oldestMs int64
	for _, cmd := range cmds {
		raw, err := cmd.Result()
		if err != nil || raw == "" {
			continue
		}
		ts, errParse := strconv.ParseInt(raw, 10, 64)
		if errParse != nil || ts <= 0 {
			continue
		}
		if oldestMs == 0 || ts < oldestMs {
			oldestMs = ts
		}
	}
	if oldestMs == 0 || oldestMs >= nowMs {
		return 0, nil
	}
	return float64(nowMs-oldestMs) / 1000.0, nil
}
