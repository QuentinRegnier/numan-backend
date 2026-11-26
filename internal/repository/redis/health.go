package redis

import (
	"context"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
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

func mapInfraTierTelemetry(snap infraRedis.TierTelemetrySnapshot) TierTelemetrySnapshot {
	shards := make([]ShardMemorySnapshot, len(snap.Shards))
	for i, s := range snap.Shards {
		shards[i] = ShardMemorySnapshot{
			ShardID:          s.ShardID,
			Role:             s.Role,
			NodeHostname:     s.NodeHostname,
			UsedBytes:        s.UsedBytes,
			MaxBytes:         s.MaxBytes,
			FillRatio:        s.FillRatio,
			EvictedKeys:      s.EvictedKeys,
			ConnectedClients: s.ConnectedClients,
		}
	}
	return TierTelemetrySnapshot{
		Tier:             snap.Tier,
		Mode:             snap.Mode,
		Dedicated:        snap.Dedicated,
		Shards:           shards,
		TotalUsedBytes:   snap.TotalUsedBytes,
		TotalMaxBytes:    snap.TotalMaxBytes,
		TotalEvictedKeys: snap.TotalEvictedKeys,
		PubSubChannels:   snap.PubSubChannels,
	}
}

// CollectAllTiersTelemetry retourne les statistiques agrégées (mémoire, shards, évictions, Pub/Sub) des 3 tiers Redis
// via les types du repository (sans fuiter internal/infrastructure/redis).
func CollectAllTiersTelemetry(ctx context.Context) (core, objects, pubsub TierTelemetrySnapshot) {
	return mapInfraTierTelemetry(infraRedis.CollectTierTelemetry(ctx, "core", infraRedis.CoreClient(), true)),
		mapInfraTierTelemetry(infraRedis.CollectTierTelemetry(ctx, "objects", infraRedis.ObjectClient(), infraRedis.HasDedicatedObjectPool())),
		mapInfraTierTelemetry(infraRedis.CollectTierTelemetry(ctx, "pubsub", infraRedis.PubSubClient(), infraRedis.HasDedicatedPubSub()))
}
