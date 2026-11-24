package redis

import (
	"context"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	goredis "github.com/redis/go-redis/v9"
)

// PingCore vérifie la connectivité du Tier 1 Redis Core.
func PingCore(ctx context.Context) error {
	core := infraRedis.CoreClient()
	if core == nil {
		return numan_error.NewAppError(http.StatusServiceUnavailable, numan_error.CodeCacheError, "Client Redis Core non initialisé.", nil)
	}
	if err := core.Ping(ctx).Err(); err != nil {
		return numan_error.NewAppError(http.StatusServiceUnavailable, numan_error.CodeCacheError, "Redis Core indisponible.", err)
	}
	return nil
}

// PingObjects vérifie la connectivité du Tier 2 Redis Object Pool.
func PingObjects(ctx context.Context) error {
	obj := infraRedis.ObjectClient()
	if obj == nil {
		return numan_error.NewAppError(http.StatusServiceUnavailable, numan_error.CodeCacheError, "Client Redis Object Pool non initialisé.", nil)
	}
	if err := obj.Ping(ctx).Err(); err != nil {
		return numan_error.NewAppError(http.StatusServiceUnavailable, numan_error.CodeCacheError, "Redis Object Pool indisponible.", err)
	}
	return nil
}

// PingPubSub vérifie la connectivité du Tier 3 Redis Pub/Sub.
func PingPubSub(ctx context.Context) error {
	ps := infraRedis.PubSubClient()
	if ps == nil {
		return numan_error.NewAppError(http.StatusServiceUnavailable, numan_error.CodeCacheError, "Client Redis Pub/Sub non initialisé.", nil)
	}
	if err := ps.Ping(ctx).Err(); err != nil {
		return numan_error.NewAppError(http.StatusServiceUnavailable, numan_error.CodeCacheError, "Redis Pub/Sub indisponible.", err)
	}
	return nil
}

// HasDedicatedObjectPool indique si Tier 2 utilise une instance/cluster distinct de Tier 1.
func HasDedicatedObjectPool() bool {
	return infraRedis.HasDedicatedObjectPool()
}

// HasDedicatedPubSub indique si Tier 3 utilise une instance/cluster distinct de Tier 1.
func HasDedicatedPubSub() bool {
	return infraRedis.HasDedicatedPubSub()
}

// CollectAllTiersTelemetry retourne les statistiques agrégées (mémoire, shards, évictions, Pub/Sub) des 3 tiers Redis.
func CollectAllTiersTelemetry(ctx context.Context) (core, objects, pubsub infraRedis.TierTelemetrySnapshot) {
	return infraRedis.CollectTierTelemetry(ctx, "core", infraRedis.CoreClient(), true),
		infraRedis.CollectTierTelemetry(ctx, "objects", infraRedis.ObjectClient(), infraRedis.HasDedicatedObjectPool()),
		infraRedis.CollectTierTelemetry(ctx, "pubsub", infraRedis.PubSubClient(), infraRedis.HasDedicatedPubSub())
}

// ScanKeysOnCore exécute un SCAN cluster-safe sur Tier 1 Redis Core pour le motif fourni.
func ScanKeysOnCore(ctx context.Context, pattern string, count int64) ([]string, error) {
	return ScanKeys(ctx, pattern, count)
}

// DeleteKeyOnCore supprime une clé complète sur Tier 1 Redis Core.
func DeleteKeyOnCore(ctx context.Context, fullKey string) error {
	core := infraRedis.CoreClient()
	if core == nil {
		return nil
	}
	return core.Del(ctx, fullKey).Err()
}

// ZRemRangeByScoreOnCore supprime un intervalle de scores d'un ZSET sur Tier 1 Redis Core.
func ZRemRangeByScoreOnCore(ctx context.Context, fullKey, minScore, maxScore string) (int64, error) {
	core := infraRedis.CoreClient()
	if core == nil {
		return 0, nil
	}
	return core.ZRemRangeByScore(ctx, fullKey, minScore, maxScore).Result()
}

// ZCardOnCore retourne la cardinalité d'un ZSET sur Tier 1 Redis Core.
func ZCardOnCore(ctx context.Context, fullKey string) (int64, error) {
	core := infraRedis.CoreClient()
	if core == nil {
		return 0, nil
	}
	return core.ZCard(ctx, fullKey).Result()
}

// ZRangeOnCore retourne une tranche de membres d'un ZSET sur Tier 1 Redis Core.
func ZRangeOnCore(ctx context.Context, fullKey string, start, stop int64) ([]string, error) {
	core := infraRedis.CoreClient()
	if core == nil {
		return nil, nil
	}
	return core.ZRange(ctx, fullKey, start, stop).Result()
}

// ZRevRangeOnCore retourne une tranche inversée de membres d'un ZSET sur Tier 1 Redis Core.
func ZRevRangeOnCore(ctx context.Context, fullKey string, start, stop int64) ([]string, error) {
	core := infraRedis.CoreClient()
	if core == nil {
		return nil, nil
	}
	return core.ZRevRange(ctx, fullKey, start, stop).Result()
}

// ZRemOnCore supprime un ou plusieurs membres d'un ZSET sur Tier 1 Redis Core.
func ZRemOnCore(ctx context.Context, fullKey string, members ...any) (int64, error) {
	core := infraRedis.CoreClient()
	if core == nil {
		return 0, nil
	}
	return core.ZRem(ctx, fullKey, members...).Result()
}

// SMembersOnCore retourne les membres d'un SET sur Tier 1 Redis Core.
func SMembersOnCore(ctx context.Context, fullKey string) ([]string, error) {
	core := infraRedis.CoreClient()
	if core == nil {
		return nil, nil
	}
	return core.SMembers(ctx, fullKey).Result()
}

// ZAddOnCore ajoute un membre scoré dans un ZSET sur Tier 1 Redis Core.
func ZAddOnCore(ctx context.Context, fullKey string, score float64, member any) error {
	core := infraRedis.CoreClient()
	if core == nil {
		return nil
	}
	return core.ZAdd(ctx, fullKey, goredis.Z{Score: score, Member: member}).Err()
}

// SetTTLOnCore définit un TTL sur une clé de Tier 1 Redis Core.
func SetTTLOnCore(ctx context.Context, fullKey string, ttlSeconds int64) error {
	core := infraRedis.CoreClient()
	if core == nil {
		return nil
	}
	return core.Do(ctx, "EXPIRE", fullKey, ttlSeconds).Err()
}

// GetBytesOnCore retourne la valeur brute d'une clé complète sur Tier 1 Redis Core.
func GetBytesOnCore(ctx context.Context, fullKey string) ([]byte, error) {
	core := infraRedis.CoreClient()
	if core == nil {
		return nil, nil
	}
	return core.Get(ctx, fullKey).Bytes()
}

// GetBytesBatchOnCore lit en pipeline plusieurs clés complètes sur Tier 1 Redis Core.
func GetBytesBatchOnCore(ctx context.Context, fullKeys []string) ([][]byte, error) {
	core := infraRedis.CoreClient()
	if core == nil || len(fullKeys) == 0 {
		return nil, nil
	}
	pipe := core.Pipeline()
	cmds := make([]*goredis.StringCmd, len(fullKeys))
	for i, k := range fullKeys {
		cmds[i] = pipe.Get(ctx, k)
	}
	_, _ = pipe.Exec(ctx)

	out := make([][]byte, 0, len(fullKeys))
	for _, cmd := range cmds {
		raw, err := cmd.Bytes()
		if err != nil || len(raw) == 0 {
			continue
		}
		out = append(out, raw)
	}
	return out, nil
}

// SRemOnCore supprime un ou plusieurs membres d'un SET sur Tier 1 Redis Core.
func SRemOnCore(ctx context.Context, fullKey string, members ...any) (int64, error) {
	core := infraRedis.CoreClient()
	if core == nil {
		return 0, nil
	}
	return core.SRem(ctx, fullKey, members...).Result()
}

// HGetAllOnObjects retourne toutes les entrées d'un HASH sur Tier 2 Redis Object Pool.
func HGetAllOnObjects(ctx context.Context, fullKey string) (map[string]string, error) {
	obj := infraRedis.ObjectClient()
	if obj == nil {
		return nil, nil
	}
	return obj.HGetAll(ctx, fullKey).Result()
}

// LLenOnCore retourne la longueur d'une LIST sur Tier 1 Redis Core.
func LLenOnCore(ctx context.Context, fullKey string) (int64, error) {
	core := infraRedis.CoreClient()
	if core == nil {
		return 0, nil
	}
	return core.LLen(ctx, fullKey).Result()
}

// LPopOnCore dépile le premier élément d'une LIST sur Tier 1 Redis Core.
func LPopOnCore(ctx context.Context, fullKey string) (string, error) {
	core := infraRedis.CoreClient()
	if core == nil {
		return "", nil
	}
	return core.LPop(ctx, fullKey).Result()
}

// RPushOnCore empile un ou plusieurs éléments en fin de LIST sur Tier 1 Redis Core.
func RPushOnCore(ctx context.Context, fullKey string, values ...any) error {
	core := infraRedis.CoreClient()
	if core == nil || len(values) == 0 {
		return nil
	}
	return core.RPush(ctx, fullKey, values...).Err()
}
