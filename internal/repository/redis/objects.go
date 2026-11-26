package redis

import (
	"context"
	"errors"
	"time"

	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	goredis "github.com/redis/go-redis/v9"
	"github.com/vmihailenco/msgpack/v5"
)

// ============================================================================
// PRIMITIVES OBJET (MsgPack)
// ============================================================================

// SetObject stocke une struct Go en MsgPack dans Redis avec le TTL par défaut.
func (c *Collection) SetObject(ctx context.Context, id any, data any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	msgpackBytes, err := msgpack.Marshal(data)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, c.Key(id), msgpackBytes, c.DefaultTTL).Err()
}

// GetObject récupère un objet et le désérialise depuis MsgPack dans 'dest'.
func (c *Collection) GetObject(ctx context.Context, id any, dest any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	val, err := c.client.Get(ctx, c.Key(id)).Bytes()
	if err != nil {
		return err // ErrNotFound si absent
	}
	return msgpack.Unmarshal(val, dest)
}

// ============================================================================
// PRIMITIVES BRUTES (Strings, Bytes & Entiers)
// ============================================================================

// SetPrimitive stocke une valeur brute sans MsgPack avec le TTL par défaut.
func (c *Collection) SetPrimitive(ctx context.Context, id any, val any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	return c.client.Set(ctx, c.Key(id), val, c.DefaultTTL).Err()
}

// SetRawWithTTL stocke une valeur brute (JSON/string/bytes) avec un TTL explicite.
func (c *Collection) SetRawWithTTL(ctx context.Context, id any, val any, ttl time.Duration) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	return c.client.Set(ctx, c.Key(id), val, ttl).Err()
}

// GetBytes récupère la valeur brute d'une clé de la collection.
func (c *Collection) GetBytes(ctx context.Context, id any) ([]byte, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	return c.client.Get(ctx, c.Key(id)).Bytes()
}

// GetInt64 récupère une valeur primitive brute sous forme d'entier.
func (c *Collection) GetInt64(ctx context.Context, id any) (int64, error) {
	if err := c.checkReady(); err != nil {
		return 0, err
	}
	return c.client.Get(ctx, c.Key(id)).Int64()
}

// Incr incrémente une valeur numérique et retourne le nouveau compteur.
func (c *Collection) Incr(ctx context.Context, id any) (int64, error) {
	if err := c.checkReady(); err != nil {
		return 0, err
	}
	return c.client.Incr(ctx, c.Key(id)).Result()
}

// SetNX pose une valeur atomiquement uniquement si la clé n'existe pas encore (Idempotence / Anti-Replay).
func (c *Collection) SetNX(ctx context.Context, id any, val any, ttl time.Duration) (bool, error) {
	if err := c.checkReady(); err != nil {
		return false, err
	}
	if ttl <= 0 {
		ttl = c.DefaultTTL
	}
	return c.client.SetNX(ctx, c.Key(id), val, ttl).Result()
}

// ============================================================================
// HELPERS STRING / OBJET BRUTS — CORE (BEST-EFFORT) & OBJECT POOL (FAIL-FAST)
// ============================================================================

// GetBytesOnCore retourne la valeur brute d'une clé complète sur Tier 1 Redis Core (best-effort : nil, nil si non initialisé).
func GetBytesOnCore(ctx context.Context, fullKey string) ([]byte, error) {
	core := redisgo.CoreClient()
	if core == nil {
		return nil, nil
	}
	return core.Get(ctx, fullKey).Bytes()
}

// GetBytesBatchOnCore lit en pipeline (lots bornés, Cluster-safe) plusieurs clés complètes sur Tier 1 Redis Core.
// Ignore les clés absentes (ErrNotFound) et remonte les erreurs réelles (best-effort : nil, nil si non initialisé).
func GetBytesBatchOnCore(ctx context.Context, fullKeys []string) ([][]byte, error) {
	core := redisgo.CoreClient()
	if core == nil || len(fullKeys) == 0 {
		return nil, nil
	}

	out := make([][]byte, 0, len(fullKeys))
	for start := 0; start < len(fullKeys); start += defaultBatchChunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + defaultBatchChunkSize
		if end > len(fullKeys) {
			end = len(fullKeys)
		}
		chunk := fullKeys[start:end]

		pipe := core.Pipeline()
		cmds := make([]*goredis.StringCmd, len(chunk))
		for i, k := range chunk {
			cmds[i] = pipe.Get(ctx, k)
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
			return nil, err
		}
		for _, cmd := range cmds {
			raw, err := cmd.Bytes()
			if err != nil {
				if errors.Is(err, goredis.Nil) {
					continue
				}
				return nil, err
			}
			if len(raw) == 0 {
				continue
			}
			out = append(out, raw)
		}
	}
	return out, nil
}

// SetOnObjects écrit une clé brute avec TTL explicite (0 = persistant) sur Tier 2 Redis Object Pool (fail-fast: ErrNotInitialized).
func SetOnObjects(ctx context.Context, fullKey string, value any, ttl time.Duration) error {
	obj := redisgo.ObjectClient()
	if obj == nil {
		return ErrNotInitialized
	}
	return obj.Set(ctx, fullKey, value, ttl).Err()
}
