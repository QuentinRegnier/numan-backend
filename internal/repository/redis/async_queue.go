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

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/go-redis/redis/v8"
)

// --- CONSTANTES ---

type ActionType string

const (
	ActionCreate ActionType = "ADD" // J'utilise tes termes "ADD"
	ActionUpdate ActionType = "UPD"
	ActionDelete ActionType = "DEL"
	ActionBuild  ActionType = "BUILD"
)

// EntityType : Sur quoi on agit
type EntityType string

const (
	EntityUser            EntityType = "Users"
	EntityUserSettings    EntityType = "UserSettings"
	EntitySession         EntityType = "Sessions"
	EntityRelation        EntityType = "Relations"
	EntityPost            EntityType = "Posts"
	EntityComment         EntityType = "Comments"
	EntityLike            EntityType = "Likes"
	EntityMedia           EntityType = "Media"
	EntityConversation    EntityType = "Conversations"
	EntityMembers         EntityType = "Members"
	EntityMessage         EntityType = "Messages"
	EntityMessageReaction EntityType = "MessageReactions"
	EntityView            EntityType = "Views"
	EntityFeed            EntityType = "Feeds"
	EntityReport          EntityType = "Reports"
	EntityTelemetry       EntityType = "Telemetry" // NOUVEAU : Pour router les Dwell Times et Clics
	EntitySaved           EntityType = "Saved"
	EntityPasskey         EntityType = "Passkeys"
	EntityNotification    EntityType = "Notifications"
)

// DBTarget : Bitmask pour savoir où envoyer (Mongo, Postgres, ou les deux)
type DBTarget int

const (
	TargetMongo    DBTarget = 1 << 0
	TargetPostgres DBTarget = 1 << 1
	TargetWorker   DBTarget = 1 << 2 // <-- NOUVEAU : Cible pour les tâches internes de calcul (hors BDD)
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

	// queueBasePrefix est le préfixe utilisé pour séparer les files par Type et Action.
	// Format de la liste : q:{shardID}:{Type}:{Action}
	queueBasePrefix = "request_cache:q:"

	// statsBasePrefix est le préfixe utilisé pour les métriques du Dashboard.
	// Format : h:stats:{shardID}
	statsBasePrefix = "request_cache:stats:"
)

// EnqueueDB : Ajout du paramètre partitionKey (int64)
// Si partitionKey est 0, on utilise l'ID de l'objet pour choisir la file.
// Si partitionKey est fourni (ex: ID du User), on utilise ça pour forcer la file.
func EnqueueDB(ctx context.Context, id int64, partitionKey int64, entity EntityType, action ActionType, data interface{}, target DBTarget) error {

	now := time.Now().UnixMilli()
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

	// C'EST ICI QUE TOUT SE JOUE : Choix du Shard
	keyForSharding := id
	if partitionKey != 0 {
		keyForSharding = partitionKey
	}
	shardID := getShardID(keyForSharding)

	shardStr := strconv.Itoa(int(shardID))
	queueKey := fmt.Sprintf("%s%s", queueBasePrefix, shardStr) // ex: q:14
	statsKey := statsBasePrefix + shardStr
	countField := "count"
	tsField := "ts"

	pipe := redisgo.Rdb.Pipeline()
	pipe.RPush(ctx, queueKey, bytes)
	pipe.HIncrBy(ctx, statsKey, countField, 1)
	pipe.HSetNX(ctx, statsKey, tsField, now)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return numan_error.NewInternal(err)
	}

	return nil
}

// --- OUTILS POUR LE WORKER INTELLIGENT ---

// PopSmartBatchBlocking attend une donnée sans consommer de CPU, puis rafle jusqu'à batchSize éléments.
func PopSmartBatchBlocking(ctx context.Context, shardID int, batchSize int64) ([]AsyncEvent, error) {
	shardStr := strconv.Itoa(shardID)
	queueKey := fmt.Sprintf("%s%s", queueBasePrefix, shardStr) // q:14
	statsKey := statsBasePrefix + shardStr

	// 1. Attente BLOQUANTE du tout premier élément (Timeout de 2s pour pouvoir écouter le ctx.Done du serveur)
	blpopRes, err := redisgo.Rdb.BLPop(ctx, 2*time.Second, queueKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) { // Timeout de 2s atteint, file toujours vide
			return nil, nil
		}
		return nil, err // Vraie erreur (ou context annulé)
	}

	// blpopRes[0] est la clé, blpopRes[1] est la valeur
	rawElements := []string{blpopRes[1]}

	// 2. On rafle le reste de la file d'un coup (Non-bloquant)
	if batchSize > 1 {
		rest, err := redisgo.Rdb.LPopCount(ctx, queueKey, int(batchSize)-1).Result()
		if err == nil && len(rest) > 0 {
			rawElements = append(rawElements, rest...)
		} else if err != nil && !errors.Is(err, redis.Nil) {
			numan_log.Warn(ctx).Err(err).Str("queue_key", queueKey).Msg("Erreur LPopCount secondaire")
		}
	}

	countPopped := int64(len(rawElements))

	// 3. Désérialiser avec PROTECTION DES INT64
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

	// 4. Mise à jour du Dashboard (les stats) en asynchrone
	go updateDashboardAfterPop(context.Background(), statsKey, queueKey, countPopped)

	return events, nil
}

// updateDashboardAfterPop gère la cohérence du tableau de bord (Async pour perf)
func updateDashboardAfterPop(ctx context.Context, statsKey, queueKey string, poppedCnt int64) {
	pipe := redisgo.Rdb.Pipeline()

	// Décrémente le compteur global du shard
	pipe.HIncrBy(ctx, statsKey, "count", -poppedCnt)

	// LINDEX key 0 nous donne le premier élément sans le supprimer
	lindexCmd := pipe.LIndex(ctx, queueKey, 0)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return
	}

	nextHeadVal, err := lindexCmd.Result()
	if err == nil && nextHeadVal != "" {
		var nextEvt AsyncEvent
		if json.Unmarshal([]byte(nextHeadVal), &nextEvt) == nil {
			redisgo.Rdb.HSet(ctx, statsKey, "ts", nextEvt.Timestamp)
		}
	} else {
		// La liste est vide
		redisgo.Rdb.HDel(ctx, statsKey, "ts")
		redisgo.Rdb.HSet(ctx, statsKey, "count", 0)
	}
}

// getShardID (Inchangé)
func getShardID(id int64) uint32 {
	idStr := strconv.FormatInt(id, 10)
	return crc32.ChecksumIEEE([]byte(idStr)) % QueueShards
}
