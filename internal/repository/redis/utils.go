package redis

import (
	"context"
	"time"

	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	goredis "github.com/redis/go-redis/v9"
)

// Exists vérifie rapidement si une clé brute est présente sur Redis Core (O(1)).
func Exists(ctx context.Context, key string) (bool, error) {
	client := redisgo.CoreClient()
	if client == nil {
		return false, nil
	}
	count, err := client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ExistsOnObjects vérifie si une clé brute est présente sur Tier 2 Redis Object Pool (repli Core).
func ExistsOnObjects(ctx context.Context, key string) (bool, error) {
	client := redisgo.ObjectClient()
	if client == nil {
		return false, nil
	}
	count, err := client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Expire pose une durée de vie sur une clé Redis Core.
func Expire(ctx context.Context, key string, expiration time.Duration) error {
	client := redisgo.CoreClient()
	if client == nil {
		return nil
	}
	return client.Expire(ctx, key, expiration).Err()
}

// DelKeysOnClient supprime un ensemble de clés sur un client donné sans erreur CROSSSLOT en mode Cluster.
func DelKeysOnClient(ctx context.Context, client goredis.UniversalClient, keys ...string) error {
	if client == nil || len(keys) == 0 {
		return nil
	}
	return delKeysClusterSafe(ctx, client, keys)
}
