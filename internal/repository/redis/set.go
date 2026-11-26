package redis

import (
	"context"
	"errors"

	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	goredis "github.com/redis/go-redis/v9"
)

func (c *Collection) SAdd(ctx context.Context, id any, members ...any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(members) == 0 {
		return nil
	}
	return c.client.SAdd(ctx, c.Key(id), members...).Err()
}

func (c *Collection) SRem(ctx context.Context, id any, members ...any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(members) == 0 {
		return nil
	}
	return c.client.SRem(ctx, c.Key(id), members...).Err()
}

func (c *Collection) SMembers(ctx context.Context, id any) ([]string, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	return c.client.SMembers(ctx, c.Key(id)).Result()
}

func (c *Collection) SIsMember(ctx context.Context, id any, member any) (bool, error) {
	if err := c.checkReady(); err != nil {
		return false, err
	}
	return c.client.SIsMember(ctx, c.Key(id), member).Result()
}

func (c *Collection) SCard(ctx context.Context, id any) (int64, error) {
	if err := c.checkReady(); err != nil {
		return 0, err
	}
	return c.client.SCard(ctx, c.Key(id)).Result()
}

func (c *Collection) SAddCount(ctx context.Context, id any, members ...any) (int64, error) {
	if err := c.checkReady(); err != nil {
		return 0, err
	}
	if len(members) == 0 {
		return 0, nil
	}
	return c.client.SAdd(ctx, c.Key(id), members...).Result()
}

func (c *Collection) SRemCount(ctx context.Context, id any, members ...any) (int64, error) {
	if err := c.checkReady(); err != nil {
		return 0, err
	}
	if len(members) == 0 {
		return 0, nil
	}
	return c.client.SRem(ctx, c.Key(id), members...).Result()
}

// SInter effectue l'intersection Redis SINTER sur un ou plusieurs identifiants de la collection (Cluster-safe).
func (c *Collection) SInter(ctx context.Context, ids ...any) ([]string, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []string{}, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = c.Key(id)
	}
	if redisgo.IsClusterClient(c.client) && len(keys) > 1 {
		pipe := c.client.Pipeline()
		cmds := make([]*goredis.StringSliceCmd, len(keys))
		for i, k := range keys {
			cmds[i] = pipe.SMembers(ctx, k)
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
			return nil, err
		}
		first, err := cmds[0].Result()
		if err != nil {
			if errors.Is(err, goredis.Nil) {
				return []string{}, nil
			}
			return nil, err
		}
		if len(first) == 0 {
			return []string{}, nil
		}
		common := make(map[string]struct{}, len(first))
		for _, m := range first {
			common[m] = struct{}{}
		}
		for _, cmd := range cmds[1:] {
			members, errM := cmd.Result()
			if errM != nil {
				if errors.Is(errM, goredis.Nil) {
					return []string{}, nil
				}
				return nil, errM
			}
			if len(members) == 0 {
				return []string{}, nil
			}
			next := make(map[string]struct{}, len(common))
			for _, m := range members {
				if _, ok := common[m]; ok {
					next[m] = struct{}{}
				}
			}
			common = next
			if len(common) == 0 {
				return []string{}, nil
			}
		}
		out := make([]string, 0, len(common))
		for m := range common {
			out = append(out, m)
		}
		return out, nil
	}
	return c.client.SInter(ctx, keys...).Result()
}

// SAddBatch insère en pipeline des lots de membres int64 dans plusieurs SETs (Cluster-safe, chunké).
func (c *Collection) SAddBatch(ctx context.Context, entries map[int64][]int64) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}

	type saddOp struct {
		key  string
		args []any
	}
	ops := make([]saddOp, 0, len(entries))
	for id, members := range entries {
		if id <= 0 || len(members) == 0 {
			continue
		}
		args := make([]any, len(members))
		for i, m := range members {
			args[i] = m
		}
		ops = append(ops, saddOp{key: c.Key(id), args: args})
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
			cmds[i] = pipe.SAdd(ctx, op.key, op.args...)
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

// SMembersBatch lit en pipeline les membres de plusieurs SETs identifiés par int64 (Cluster-safe, chunké).
// Déduplique les IDs d'entrée ; les clés absentes ne provoquent pas d'erreur.
func (c *Collection) SMembersBatch(ctx context.Context, ids []int64) (map[int64][]string, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	res := make(map[int64][]string, len(ids))
	if len(ids) == 0 {
		return res, nil
	}

	uniqueIDs := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}

	for start := 0; start < len(uniqueIDs); start += defaultBatchChunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + defaultBatchChunkSize
		if end > len(uniqueIDs) {
			end = len(uniqueIDs)
		}
		chunk := uniqueIDs[start:end]

		pipe := c.client.Pipeline()
		cmds := make([]*goredis.StringSliceCmd, len(chunk))
		for i, id := range chunk {
			cmds[i] = pipe.SMembers(ctx, c.Key(id))
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
			return nil, err
		}
		for i, cmd := range cmds {
			members, err := cmd.Result()
			if err != nil {
				if errors.Is(err, goredis.Nil) {
					continue
				}
				return nil, err
			}
			res[chunk[i]] = members
		}
	}
	return res, nil
}

// SCardBatch lit en pipeline les cardinalités de plusieurs SETs identifiés par int64 (Cluster-safe, chunké).
func (c *Collection) SCardBatch(ctx context.Context, ids []int64) (map[int64]int64, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	res := make(map[int64]int64, len(ids))
	if len(ids) == 0 {
		return res, nil
	}

	uniqueIDs := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}

	for start := 0; start < len(uniqueIDs); start += defaultBatchChunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + defaultBatchChunkSize
		if end > len(uniqueIDs) {
			end = len(uniqueIDs)
		}
		chunk := uniqueIDs[start:end]

		pipe := c.client.Pipeline()
		cmds := make([]*goredis.IntCmd, len(chunk))
		for i, id := range chunk {
			cmds[i] = pipe.SCard(ctx, c.Key(id))
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
			return nil, err
		}
		for i, cmd := range cmds {
			card, err := cmd.Result()
			if err != nil {
				if errors.Is(err, goredis.Nil) {
					continue
				}
				return nil, err
			}
			res[chunk[i]] = card
		}
	}
	return res, nil
}

// ============================================================================
// HELPERS SET BRUTS — TIER 1 REDIS CORE (BEST-EFFORT: nil SI NON INITIALISÉ)
// ============================================================================

// SMembersOnCore retourne les membres d'un SET sur Tier 1 Redis Core (best-effort : nil, nil si non initialisé).
func SMembersOnCore(ctx context.Context, fullKey string) ([]string, error) {
	core := redisgo.CoreClient()
	if core == nil {
		return nil, nil
	}
	return core.SMembers(ctx, fullKey).Result()
}

// SRemOnCore supprime un ou plusieurs membres d'un SET sur Tier 1 Redis Core (best-effort : 0, nil si non initialisé).
func SRemOnCore(ctx context.Context, fullKey string, members ...any) (int64, error) {
	core := redisgo.CoreClient()
	if core == nil {
		return 0, nil
	}
	return core.SRem(ctx, fullKey, members...).Result()
}
