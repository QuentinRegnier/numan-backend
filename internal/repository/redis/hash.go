package redis

import (
	"context"
	"errors"
	"time"

	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	goredis "github.com/redis/go-redis/v9"
)

// HGet lit la valeur string d'un champ de HASH. Retourne ErrNotFound en cas d'absence.
func (c *Collection) HGet(ctx context.Context, id any, field string) (string, error) {
	if err := c.checkReady(); err != nil {
		return "", err
	}
	return c.client.HGet(ctx, c.Key(id), field).Result()
}

// HGetBytes lit la valeur binaire brute d'un champ de HASH. Retourne ErrNotFound en cas d'absence.
func (c *Collection) HGetBytes(ctx context.Context, id any, field string) ([]byte, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	return c.client.HGet(ctx, c.Key(id), field).Bytes()
}

// HSet écrit un ou plusieurs couples champ/valeur dans un HASH.
func (c *Collection) HSet(ctx context.Context, id any, values ...any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}
	return c.client.HSet(ctx, c.Key(id), values...).Err()
}

// HSetMapBatch écrit en pipeline un lot de maps champ->valeur sur plusieurs HASH de la collection (Cluster-safe, chunké).
func (c *Collection) HSetMapBatch(ctx context.Context, entries map[any]map[string]any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}

	type hsetOp struct {
		key  string
		args []any
	}
	ops := make([]hsetOp, 0, len(entries))
	for id, fields := range entries {
		if len(fields) == 0 {
			continue
		}
		args := make([]any, 0, len(fields)*2)
		for f, v := range fields {
			args = append(args, f, v)
		}
		ops = append(ops, hsetOp{key: c.Key(id), args: args})
	}
	if len(ops) == 0 {
		return nil
	}

	for start := 0; start < len(ops); start += defaultBatchChunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + defaultBatchChunkSize
		if end > len(ops) {
			end = len(ops)
		}
		chunk := ops[start:end]

		pipe := c.client.Pipeline()
		cmds := make([]*goredis.IntCmd, len(chunk))
		for i, op := range chunk {
			cmds[i] = pipe.HSet(ctx, op.key, op.args...)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
		for _, cmd := range cmds {
			if err := cmd.Err(); err != nil {
				return err
			}
		}
	}
	return nil
}

// HGetAll retourne l'intégralité des champs d'un HASH sous forme map[string]string.
func (c *Collection) HGetAll(ctx context.Context, id any) (map[string]string, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	return c.client.HGetAll(ctx, c.Key(id)).Result()
}

// HMGet lit plusieurs champs d'un HASH en un seul aller-retour.
func (c *Collection) HMGet(ctx context.Context, id any, fields ...string) ([]any, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, nil
	}
	return c.client.HMGet(ctx, c.Key(id), fields...).Result()
}

// HMGetBatch exécute HMGET avec le même ensemble de champs sur plusieurs clés de la collection via Pipeline (Cluster-safe).
// Retourne une tranche alignée positionnellement 1:1 avec `ids`.
func (c *Collection) HMGetBatch(ctx context.Context, ids []any, fields []string) ([][]any, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	if len(ids) == 0 || len(fields) == 0 {
		return make([][]any, len(ids)), nil
	}

	out := make([][]any, len(ids))
	for start := 0; start < len(ids); start += defaultBatchChunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + defaultBatchChunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]

		pipe := c.client.Pipeline()
		cmds := make([]*goredis.SliceCmd, len(chunk))
		for i, id := range chunk {
			cmds[i] = pipe.HMGet(ctx, c.Key(id), fields...)
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
			return nil, err
		}
		for i, cmd := range cmds {
			vals, err := cmd.Result()
			if err != nil {
				if errors.Is(err, goredis.Nil) {
					continue
				}
				return nil, err
			}
			out[start+i] = vals
		}
	}
	return out, nil
}

// HIncrBy incrémente la valeur entière d'un champ de HASH.
func (c *Collection) HIncrBy(ctx context.Context, id any, field string, incr int64) (int64, error) {
	if err := c.checkReady(); err != nil {
		return 0, err
	}
	return c.client.HIncrBy(ctx, c.Key(id), field, incr).Result()
}

// HIncrByWithExpireNX incrémente un champ de HASH et pose un TTL (EXPIRE ... NX avec repli EXPIRE) en un pipeline.
func (c *Collection) HIncrByWithExpireNX(ctx context.Context, id any, field string, incr int64, ttl time.Duration) (int64, error) {
	if err := c.checkReady(); err != nil {
		return 0, err
	}
	if ttl <= 0 {
		ttl = c.DefaultTTL
	}
	key := c.Key(id)
	ttlSec := int64(ttl / time.Second)
	if ttl > 0 && ttlSec <= 0 {
		ttlSec = 1
	}

	pipe := c.client.Pipeline()
	incrCmd := pipe.HIncrBy(ctx, key, field, incr)
	if ttlSec > 0 {
		pipe.Do(ctx, "EXPIRE", key, ttlSec, "NX")
	}
	if _, err := pipe.Exec(ctx); err != nil {
		// Repli tolérant si Redis < 7 rejette l'option NX de EXPIRE
		fallbackPipe := c.client.Pipeline()
		fbIncr := fallbackPipe.HIncrBy(ctx, key, field, incr)
		if ttl > 0 {
			fallbackPipe.Expire(ctx, key, ttl)
		}
		if _, fbErr := fallbackPipe.Exec(ctx); fbErr != nil {
			return 0, fbErr
		}
		return fbIncr.Result()
	}
	return incrCmd.Result()
}

// HDel supprime un ou plusieurs champs d'un HASH.
func (c *Collection) HDel(ctx context.Context, id any, fields ...string) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(fields) == 0 {
		return nil
	}
	return c.client.HDel(ctx, c.Key(id), fields...).Err()
}

// HLen retourne le nombre de champs dans un HASH.
func (c *Collection) HLen(ctx context.Context, id any) (int64, error) {
	if err := c.checkReady(); err != nil {
		return 0, err
	}
	return c.client.HLen(ctx, c.Key(id)).Result()
}

// HExists indique si un champ existe dans un HASH.
func (c *Collection) HExists(ctx context.Context, id any, field string) (bool, error) {
	if err := c.checkReady(); err != nil {
		return false, err
	}
	return c.client.HExists(ctx, c.Key(id), field).Result()
}

// HashReplaceAtomic remplace atomiquement un HASH (DEL + HSET + EXPIRE dans MULTI/EXEC).
func (c *Collection) HashReplaceAtomic(ctx context.Context, id any, fields map[string]any, ttl time.Duration) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	key := c.Key(id)
	if ttl <= 0 {
		ttl = c.DefaultTTL
	}
	pipe := c.client.TxPipeline()
	pipe.Del(ctx, key)
	if len(fields) > 0 {
		args := make([]any, 0, len(fields)*2)
		for k, v := range fields {
			args = append(args, k, v)
		}
		pipe.HSet(ctx, key, args...)
		if ttl > 0 {
			pipe.Expire(ctx, key, ttl)
		}
	}
	_, err := pipe.Exec(ctx)
	return err
}

// ============================================================================
// HELPERS HASH BRUTS — TIER 2 REDIS OBJECT POOL
// ============================================================================

// HSetOnObjects écrit un ou plusieurs couples champ/valeur dans un HASH sur Tier 2 Redis Object Pool (fail-fast: ErrNotInitialized).
func HSetOnObjects(ctx context.Context, fullKey string, values ...any) error {
	obj := redisgo.ObjectClient()
	if obj == nil {
		return ErrNotInitialized
	}
	if len(values) == 0 {
		return nil
	}
	return obj.HSet(ctx, fullKey, values...).Err()
}

// HGetOnObjects lit un champ d'un HASH sur Tier 2 Redis Object Pool (fail-fast: ErrNotInitialized).
func HGetOnObjects(ctx context.Context, fullKey, field string) (string, error) {
	obj := redisgo.ObjectClient()
	if obj == nil {
		return "", ErrNotInitialized
	}
	return obj.HGet(ctx, fullKey, field).Result()
}

// HGetAllOnObjects retourne toutes les entrées d'un HASH sur Tier 2 Redis Object Pool (best-effort : nil, nil si non initialisé).
func HGetAllOnObjects(ctx context.Context, fullKey string) (map[string]string, error) {
	obj := redisgo.ObjectClient()
	if obj == nil {
		return nil, nil
	}
	return obj.HGetAll(ctx, fullKey).Result()
}

// HMGetOnObjects lit plusieurs champs d'un HASH sur Tier 2 Redis Object Pool (fail-fast: ErrNotInitialized).
func HMGetOnObjects(ctx context.Context, fullKey string, fields ...string) ([]any, error) {
	obj := redisgo.ObjectClient()
	if obj == nil {
		return nil, ErrNotInitialized
	}
	if len(fields) == 0 {
		return nil, nil
	}
	return obj.HMGet(ctx, fullKey, fields...).Result()
}

// HDelOnObjects supprime un ou plusieurs champs d'un HASH sur Tier 2 Redis Object Pool (fail-fast: ErrNotInitialized).
func HDelOnObjects(ctx context.Context, fullKey string, fields ...string) error {
	obj := redisgo.ObjectClient()
	if obj == nil {
		return ErrNotInitialized
	}
	if len(fields) == 0 {
		return nil
	}
	return obj.HDel(ctx, fullKey, fields...).Err()
}

// HLenOnObjects retourne la cardinalité d'un HASH sur Tier 2 Redis Object Pool (fail-fast: ErrNotInitialized).
func HLenOnObjects(ctx context.Context, fullKey string) (int64, error) {
	obj := redisgo.ObjectClient()
	if obj == nil {
		return 0, ErrNotInitialized
	}
	return obj.HLen(ctx, fullKey).Result()
}

// HExistsOnObjects vérifie l'existence d'un champ dans un HASH sur Tier 2 Redis Object Pool (fail-fast: ErrNotInitialized).
func HExistsOnObjects(ctx context.Context, fullKey, field string) (bool, error) {
	obj := redisgo.ObjectClient()
	if obj == nil {
		return false, ErrNotInitialized
	}
	return obj.HExists(ctx, fullKey, field).Result()
}
