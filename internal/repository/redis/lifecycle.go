package redis

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	goredis "github.com/redis/go-redis/v9"
)

const (
	defaultScanCount int64 = 200
	maxScanCount     int64 = 2000
)

// IsReady isole l'état de l'infrastructure Redis Core pour les routines de maintenance et les services.
func IsReady() bool {
	return redisgo.CoreClient() != nil
}

// IsObjectPoolReady indique si le client Redis Tier 2 (Object Pool ou repli Core) est disponible.
func IsObjectPoolReady() bool {
	return redisgo.ObjectClient() != nil
}

// FlushDB vide l'intégralité des bases de données Redis distinctes (Cluster-safe sur tous les masters).
func FlushDB(ctx context.Context) error {
	seen := make(map[goredis.UniversalClient]struct{}, 3)
	for _, client := range []goredis.UniversalClient{redisgo.CoreClient(), redisgo.ObjectClient(), redisgo.PubSubClient()} {
		if client == nil {
			continue
		}
		if _, done := seen[client]; done {
			continue
		}
		seen[client] = struct{}{}
		if clusterClient, ok := client.(*goredis.ClusterClient); ok {
			if err := clusterClient.ForEachMaster(ctx, func(c context.Context, master *goredis.Client) error {
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

// ScanKeys itère de manière non-bloquante via SCAN sur un motif de clés sur Redis Core (Cluster-safe via ForEachMaster).
func ScanKeys(ctx context.Context, pattern string, count int64) ([]string, error) {
	return scanKeysOnClient(ctx, redisgo.CoreClient(), pattern, count)
}

// ScanKeysOnCore exécute un SCAN Cluster-safe sur Tier 1 Redis Core pour le motif fourni.
func ScanKeysOnCore(ctx context.Context, pattern string, count int64) ([]string, error) {
	return scanKeysOnClient(ctx, redisgo.CoreClient(), pattern, count)
}

// ScanKeysOnObjects exécute un SCAN Cluster-safe sur Tier 2 Redis Object Pool pour le motif fourni.
func ScanKeysOnObjects(ctx context.Context, pattern string, count int64) ([]string, error) {
	return scanKeysOnClient(ctx, redisgo.ObjectClient(), pattern, count)
}

// ScanKeysBatchedOnCore parcourt en streaming (mémoire constante) les clés correspondant au motif sur Tier 1 Core.
// Le callback fn est invoqué de façon sérialisée pour chaque page non vide ; retourner ErrStopScan arrête proprement le parcours.
func ScanKeysBatchedOnCore(ctx context.Context, pattern string, count int64, fn func(keys []string) error) error {
	return scanKeysBatchedOnClient(ctx, redisgo.CoreClient(), pattern, count, fn)
}

// ScanKeysBatchedOnObjects parcourt en streaming (mémoire constante) les clés correspondant au motif sur Tier 2 Object Pool.
// Le callback fn est invoqué de façon sérialisée pour chaque page non vide ; retourner ErrStopScan arrête proprement le parcours.
func ScanKeysBatchedOnObjects(ctx context.Context, pattern string, count int64, fn func(keys []string) error) error {
	return scanKeysBatchedOnClient(ctx, redisgo.ObjectClient(), pattern, count, fn)
}

// scanKeysOnClient itère via SCAN sur un client Redis universel (Standalone, Sentinel ou Cluster via ForEachMaster).
// Respecte l'annulation de contexte et borne la taille de page SCAN.
func scanKeysOnClient(ctx context.Context, client goredis.UniversalClient, pattern string, count int64) ([]string, error) {
	var all []string
	err := scanKeysBatchedOnClient(ctx, client, pattern, count, func(keys []string) error {
		all = append(all, keys...)
		return nil
	})
	return all, err
}

// scanKeysBatchedOnClient exécute un SCAN en streaming sur Standalone, Sentinel ou Cluster (ForEachMaster).
// Le callback fn est protégé par mutex afin de rester thread-safe même lorsque ForEachMaster parcourt plusieurs shards en parallèle.
func scanKeysBatchedOnClient(ctx context.Context, client goredis.UniversalClient, pattern string, count int64, fn func(keys []string) error) error {
	if client == nil || fn == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if count <= 0 {
		count = defaultScanCount
	}
	if count > maxScanCount {
		count = maxScanCount
	}

	if clusterClient, ok := client.(*goredis.ClusterClient); ok {
		var (
			cbMu    sync.Mutex
			stopped atomic.Bool
		)
		err := clusterClient.ForEachMaster(ctx, func(c context.Context, master *goredis.Client) error {
			var cursor uint64
			for {
				if stopped.Load() {
					return nil
				}
				if errCtx := c.Err(); errCtx != nil {
					return errCtx
				}
				keys, nextCursor, errS := master.Scan(c, cursor, pattern, count).Result()
				if errS != nil {
					return errS
				}
				if len(keys) > 0 {
					cbMu.Lock()
					var cbErr error
					if !stopped.Load() {
						cbErr = fn(keys)
						if errors.Is(cbErr, ErrStopScan) {
							stopped.Store(true)
							cbErr = nil
						}
					}
					cbMu.Unlock()
					if cbErr != nil {
						stopped.Store(true)
						return cbErr
					}
					if stopped.Load() {
						return nil
					}
				}
				cursor = nextCursor
				if cursor == 0 {
					break
				}
			}
			return nil
		})
		return err
	}

	var cursor uint64
	for {
		if errCtx := ctx.Err(); errCtx != nil {
			return errCtx
		}
		keys, nextCursor, err := client.Scan(ctx, cursor, pattern, count).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if cbErr := fn(keys); cbErr != nil {
				if errors.Is(cbErr, ErrStopScan) {
					return nil
				}
				return cbErr
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return nil
}

// ============================================================================
// OPÉRATIONS CYCLE DE VIE CLÉS BRUTES — EXISTENCE, TTL, DEL & UNLINK
// (Best-effort : retournent zéro/nil sans erreur si le client Redis n'est pas initialisé)
// ============================================================================

// Del supprime une ou plusieurs clés brutes sur Tier 1 Redis Core (Cluster-safe, best-effort : nil si non initialisé).
func Del(ctx context.Context, keys ...string) error {
	return delKeysClusterSafe(ctx, redisgo.CoreClient(), keys)
}

// Exists vérifie rapidement si une clé brute est présente sur Tier 1 Redis Core (O(1), best-effort : false, nil si non initialisé).
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

// Expire pose une durée de vie sur une clé brute de Tier 1 Redis Core (best-effort : nil si non initialisé).
func Expire(ctx context.Context, key string, expiration time.Duration) error {
	client := redisgo.CoreClient()
	if client == nil {
		return nil
	}
	return client.Expire(ctx, key, expiration).Err()
}

// SetTTLOnCore définit un TTL en secondes sur une clé de Tier 1 Redis Core (best-effort : nil si non initialisé).
func SetTTLOnCore(ctx context.Context, fullKey string, ttlSeconds int64) error {
	core := redisgo.CoreClient()
	if core == nil {
		return nil
	}
	return core.Do(ctx, "EXPIRE", fullKey, ttlSeconds).Err()
}

// DeleteKeyOnCore supprime une clé complète sur Tier 1 Redis Core (best-effort : nil si non initialisé).
func DeleteKeyOnCore(ctx context.Context, fullKey string) error {
	core := redisgo.CoreClient()
	if core == nil {
		return nil
	}
	return core.Del(ctx, fullKey).Err()
}

// DeleteKeysOnCore supprime un ensemble de clés complètes sur Tier 1 Redis Core (Cluster-safe, best-effort : nil si non initialisé).
func DeleteKeysOnCore(ctx context.Context, fullKeys ...string) error {
	return delKeysClusterSafe(ctx, redisgo.CoreClient(), fullKeys)
}

// UnlinkKeysOnCore supprime via UNLINK un ensemble de clés complètes sur Tier 1 Redis Core (Cluster-safe, best-effort : nil si non initialisé).
func UnlinkKeysOnCore(ctx context.Context, fullKeys ...string) error {
	return unlinkKeysClusterSafe(ctx, redisgo.CoreClient(), fullKeys)
}

// ExistsOnObjects vérifie si une clé brute est présente sur Tier 2 Redis Object Pool (repli Core, best-effort : false, nil si non initialisé).
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

// DeleteKeysOnObjects supprime un ensemble de clés complètes sur Tier 2 Redis Object Pool (Cluster-safe, best-effort : nil si non initialisé).
func DeleteKeysOnObjects(ctx context.Context, fullKeys ...string) error {
	return delKeysClusterSafe(ctx, redisgo.ObjectClient(), fullKeys)
}

// UnlinkKeysOnObjects supprime via UNLINK un ensemble de clés complètes sur Tier 2 Redis Object Pool (Cluster-safe, best-effort : nil si non initialisé).
func UnlinkKeysOnObjects(ctx context.Context, fullKeys ...string) error {
	return unlinkKeysClusterSafe(ctx, redisgo.ObjectClient(), fullKeys)
}
