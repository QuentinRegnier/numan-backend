package redis

import (
	"context"
	"errors"

	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	goredis "github.com/redis/go-redis/v9"
)

// ============================================================================
// 1. FONCTIONS GLOBALES ZSET — TIER 1 REDIS CORE (FAIL-FAST: ErrNotInitialized)
// ============================================================================

// ZAdd ajoute un membre scoré dans un ZSET brut sur Tier 1 Redis Core (fail-fast: ErrNotInitialized si non initialisé).
func ZAdd(ctx context.Context, key string, score float64, member interface{}) error {
	core := redisgo.CoreClient()
	if core == nil {
		return ErrNotInitialized
	}
	return core.ZAdd(ctx, key, goredis.Z{Score: score, Member: member}).Err()
}

// ZRevRange lit une tranche inversée d'un ZSET brut sur Tier 1 Redis Core (fail-fast: ErrNotInitialized).
func ZRevRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	core := redisgo.CoreClient()
	if core == nil {
		return nil, ErrNotInitialized
	}
	return core.ZRevRange(ctx, key, start, stop).Result()
}

// ZRange lit une tranche croissante d'un ZSET brut sur Tier 1 Redis Core (fail-fast: ErrNotInitialized).
func ZRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	core := redisgo.CoreClient()
	if core == nil {
		return nil, ErrNotInitialized
	}
	return core.ZRange(ctx, key, start, stop).Result()
}

// ZCard retourne la cardinalité d'un ZSET brut sur Tier 1 Redis Core (fail-fast: ErrNotInitialized).
func ZCard(ctx context.Context, key string) (int64, error) {
	core := redisgo.CoreClient()
	if core == nil {
		return 0, ErrNotInitialized
	}
	return core.ZCard(ctx, key).Result()
}

// ZRevRangeByRanks lit plusieurs rangs précis d'un ZSET brut via pipeline sur Tier 1 Redis Core (fail-fast: ErrNotInitialized).
func ZRevRangeByRanks(ctx context.Context, key string, ranks []int64) ([]string, error) {
	core := redisgo.CoreClient()
	if core == nil {
		return nil, ErrNotInitialized
	}
	if len(ranks) == 0 {
		return nil, nil
	}
	pipe := core.Pipeline()
	cmds := make([]*goredis.StringSliceCmd, 0, len(ranks))
	for _, rank := range ranks {
		cmds = append(cmds, pipe.ZRevRange(ctx, key, rank, rank))
	}
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, goredis.Nil) {
		return nil, err
	}
	results := make([]string, 0, len(ranks))
	for _, cmd := range cmds {
		res, err := cmd.Result()
		if err != nil {
			if errors.Is(err, goredis.Nil) {
				continue
			}
			return nil, err
		}
		if len(res) > 0 {
			results = append(results, res[0])
		}
	}
	return results, nil
}

// ZRevRangeWithScores lit une tranche inversée avec scores sur Tier 1 Redis Core (fail-fast: ErrNotInitialized).
func ZRevRangeWithScores(ctx context.Context, key string, start, stop int64) ([]ZMember, error) {
	core := redisgo.CoreClient()
	if core == nil {
		return nil, ErrNotInitialized
	}
	raw, err := core.ZRevRangeWithScores(ctx, key, start, stop).Result()
	if err != nil {
		return nil, err
	}
	out := make([]ZMember, len(raw))
	for i, z := range raw {
		out[i] = ZMember{
			Score:  z.Score,
			Member: z.Member,
		}
	}
	return out, nil
}

// ZScores lit plusieurs scores d'un ZSET brut en un pipeline sur Tier 1 Redis Core (fail-fast: ErrNotInitialized).
func ZScores(ctx context.Context, key string, members []string) ([]float64, error) {
	core := redisgo.CoreClient()
	if core == nil {
		return nil, ErrNotInitialized
	}
	if len(members) == 0 {
		return nil, nil
	}
	pipe := core.Pipeline()
	cmds := make([]*goredis.FloatCmd, len(members))
	for i, member := range members {
		cmds[i] = pipe.ZScore(ctx, key, member)
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
		return nil, err
	}
	scores := make([]float64, len(members))
	for i, cmd := range cmds {
		val, errCmd := cmd.Result()
		if errCmd != nil {
			if errors.Is(errCmd, goredis.Nil) {
				scores[i] = 0
				continue
			}
			return nil, errCmd
		}
		scores[i] = val
	}
	return scores, nil
}

// ============================================================================
// 2. HELPERS ZSET BRUTS — TIER 1 REDIS CORE (BEST-EFFORT: nil SI NON INITIALISÉ)
// ============================================================================

// ZRemRangeByScoreOnCore supprime un intervalle de scores d'un ZSET sur Tier 1 Redis Core (best-effort : 0, nil si non initialisé).
func ZRemRangeByScoreOnCore(ctx context.Context, fullKey, minScore, maxScore string) (int64, error) {
	core := redisgo.CoreClient()
	if core == nil {
		return 0, nil
	}
	return core.ZRemRangeByScore(ctx, fullKey, minScore, maxScore).Result()
}

// ZCardOnCore retourne la cardinalité d'un ZSET sur Tier 1 Redis Core (best-effort : 0, nil si non initialisé).
func ZCardOnCore(ctx context.Context, fullKey string) (int64, error) {
	core := redisgo.CoreClient()
	if core == nil {
		return 0, nil
	}
	return core.ZCard(ctx, fullKey).Result()
}

// ZRangeOnCore retourne une tranche de membres d'un ZSET sur Tier 1 Redis Core (best-effort : nil, nil si non initialisé).
func ZRangeOnCore(ctx context.Context, fullKey string, start, stop int64) ([]string, error) {
	core := redisgo.CoreClient()
	if core == nil {
		return nil, nil
	}
	return core.ZRange(ctx, fullKey, start, stop).Result()
}

// ZRemOnCore supprime un ou plusieurs membres d'un ZSET sur Tier 1 Redis Core (best-effort : 0, nil si non initialisé).
func ZRemOnCore(ctx context.Context, fullKey string, members ...any) (int64, error) {
	core := redisgo.CoreClient()
	if core == nil {
		return 0, nil
	}
	return core.ZRem(ctx, fullKey, members...).Result()
}

// ZAddOnCore ajoute un membre scoré dans un ZSET sur Tier 1 Redis Core (best-effort : nil si non initialisé).
func ZAddOnCore(ctx context.Context, fullKey string, score float64, member any) error {
	core := redisgo.CoreClient()
	if core == nil {
		return nil
	}
	return core.ZAdd(ctx, fullKey, goredis.Z{Score: score, Member: member}).Err()
}

// ============================================================================
// 3. HELPERS ZSET BRUTS — TIER 2 REDIS OBJECT POOL (FAIL-FAST: ErrNotInitialized)
// ============================================================================

// ZAddOnObjects ajoute un membre scoré dans un ZSET sur Tier 2 Redis Object Pool (fail-fast: ErrNotInitialized).
func ZAddOnObjects(ctx context.Context, fullKey string, score float64, member any) error {
	obj := redisgo.ObjectClient()
	if obj == nil {
		return ErrNotInitialized
	}
	return obj.ZAdd(ctx, fullKey, goredis.Z{Score: score, Member: member}).Err()
}

// ZRemOnObjects supprime un ou plusieurs membres d'un ZSET sur Tier 2 Redis Object Pool (fail-fast: ErrNotInitialized).
func ZRemOnObjects(ctx context.Context, fullKey string, members ...any) error {
	obj := redisgo.ObjectClient()
	if obj == nil {
		return ErrNotInitialized
	}
	if len(members) == 0 {
		return nil
	}
	return obj.ZRem(ctx, fullKey, members...).Err()
}

// ZRevRangeOnObjects lit une tranche inversée d'un ZSET sur Tier 2 Redis Object Pool (fail-fast: ErrNotInitialized).
func ZRevRangeOnObjects(ctx context.Context, fullKey string, start, stop int64) ([]string, error) {
	obj := redisgo.ObjectClient()
	if obj == nil {
		return nil, ErrNotInitialized
	}
	return obj.ZRevRange(ctx, fullKey, start, stop).Result()
}
