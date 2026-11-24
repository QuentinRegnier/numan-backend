package cache_service

import (
	"context"

	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// PingRedisCore vérifie la connectivité du Tier 1 Redis Core.
func PingRedisCore(ctx context.Context) error {
	return repoRedis.PingCore(ctx)
}

// PingRedisObjects vérifie la connectivité du Tier 2 Redis Object Pool.
func PingRedisObjects(ctx context.Context) error {
	return repoRedis.PingObjects(ctx)
}

// PingRedisPubSub vérifie la connectivité du Tier 3 Redis Pub/Sub.
func PingRedisPubSub(ctx context.Context) error {
	return repoRedis.PingPubSub(ctx)
}

// HasDedicatedObjectPool indique si le Tier 2 Redis Object Pool utilise une topologie dédiée distincte de Core.
func HasDedicatedObjectPool() bool {
	return repoRedis.HasDedicatedObjectPool()
}

// HasDedicatedPubSub indique si le Tier 3 Redis Pub/Sub utilise une topologie dédiée distincte de Core.
func HasDedicatedPubSub() bool {
	return repoRedis.HasDedicatedPubSub()
}
