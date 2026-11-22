package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
)

const (
	clusterDatacentersKey = "cluster:datacenters"
	clusterNodesIndexFmt  = "cluster:nodes:%s"
	clusterNodeEntryFmt   = "cluster:node:%s:%s"
)

// RegisterClusterNode enregistre ou renouvelle la fiche d'un nœud dans Redis L1 avec un TTL strict.
func RegisterClusterNode(ctx context.Context, datacenter, memberID string, payload any, ttl time.Duration) error {
	if redisgo.Rdb == nil {
		return errors.New("redis client not initialized")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	indexKey := fmt.Sprintf(clusterNodesIndexFmt, datacenter)
	entryKey := fmt.Sprintf(clusterNodeEntryFmt, datacenter, memberID)

	pipe := redisgo.Rdb.Pipeline()
	pipe.SAdd(ctx, clusterDatacentersKey, datacenter)
	pipe.SAdd(ctx, indexKey, memberID)
	pipe.Set(ctx, entryKey, raw, ttl)
	_, err = pipe.Exec(ctx)
	return err
}

// UnregisterClusterNode retire immédiatement un nœud du registre (appelé au Graceful Shutdown).
func UnregisterClusterNode(ctx context.Context, datacenter, memberID string) error {
	if redisgo.Rdb == nil {
		return nil
	}
	indexKey := fmt.Sprintf(clusterNodesIndexFmt, datacenter)
	entryKey := fmt.Sprintf(clusterNodeEntryFmt, datacenter, memberID)

	pipe := redisgo.Rdb.Pipeline()
	pipe.Del(ctx, entryKey)
	pipe.SRem(ctx, indexKey, memberID)
	_, err := pipe.Exec(ctx)
	return err
}

// ListClusterNodePayloads retourne les fiches JSON actives d'un datacenter et purge automatiquement les entrées expirées.
func ListClusterNodePayloads(ctx context.Context, datacenter string) ([][]byte, error) {
	if redisgo.Rdb == nil {
		return nil, errors.New("redis client not initialized")
	}
	indexKey := fmt.Sprintf(clusterNodesIndexFmt, datacenter)
	members, err := redisgo.Rdb.SMembers(ctx, indexKey).Result()
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, nil
	}

	keys := make([]string, len(members))
	for i, m := range members {
		keys[i] = fmt.Sprintf(clusterNodeEntryFmt, datacenter, m)
	}

	values, err := redisgo.Rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	payloads := make([][]byte, 0, len(values))
	var stale []any
	for i, v := range values {
		if v == nil {
			stale = append(stale, members[i])
			continue
		}
		if s, ok := v.(string); ok {
			payloads = append(payloads, []byte(s))
		}
	}

	if len(stale) > 0 {
		_ = redisgo.Rdb.SRem(ctx, indexKey, stale...).Err()
	}

	return payloads, nil
}

// ListRegisteredDatacenters retourne les identifiants de datacenters enregistrés dans ce Redis.
func ListRegisteredDatacenters(ctx context.Context) ([]string, error) {
	if redisgo.Rdb == nil {
		return nil, errors.New("redis client not initialized")
	}
	return redisgo.Rdb.SMembers(ctx, clusterDatacentersKey).Result()
}
