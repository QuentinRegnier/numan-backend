package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/redis/go-redis/v9"
)

// ============================================================================
// 1. LUA SCRIPTS
// ============================================================================

// zaddCapScript garantit l'atomicité de l'insertion, du plafonnement et de l'application du TTL.
// TDD §3.3 : Utilise un script Lua précompilé (EVALSHA) pour éviter toute race condition et fuite mémoire.
const zaddCapScript = `
local key = KEYS[1]
local score = tonumber(ARGV[1])
local member = ARGV[2]
local max_size = tonumber(ARGV[3])
local ttl_seconds = tonumber(ARGV[4])

redis.call('ZADD', key, score, member)
local current_size = redis.call('ZCARD', key)

if current_size > max_size then
   redis.call('ZREMRANGEBYRANK', key, 0, current_size - max_size - 1)
end

if ttl_seconds and ttl_seconds > 0 then
   redis.call('EXPIRE', key, ttl_seconds)
end

return redis.call('ZSCORE', key, member)
`

var zaddCapCompiled = redis.NewScript(zaddCapScript)

// ============================================================================
// 2. MÉTHODES DDD DE LA STRUCTURE "Collection" (Nouvelle Architecture)
// ============================================================================

// ZAdd ajoute un élément avec un score (ou met à jour son score).
func (c *Collection) ZAdd(ctx context.Context, id any, score float64, member any) error {
	return c.Client.ZAdd(ctx, c.Key(id), redis.Z{
		Score:  score,
		Member: member,
	}).Err()
}

// ZAddWithCap insère un élément, plafonne le ZSET et applique le DefaultTTL de la collection en une seule passe atomique via Lua (EVALSHA).
func (c *Collection) ZAddWithCap(ctx context.Context, id any, score float64, member any, maxSize int) error {
	ttlSeconds := int64(c.DefaultTTL.Seconds())
	return zaddCapCompiled.Run(ctx, c.Client, []string{c.Key(id)}, score, member, maxSize, ttlSeconds).Err()
}

// ZAddWithCapAndTTL insère un élément, plafonne le ZSET et applique un TTL explicite en une seule passe atomique via Lua (EVALSHA).
func (c *Collection) ZAddWithCapAndTTL(ctx context.Context, id any, score float64, member any, maxSize int, ttl time.Duration) error {
	ttlSeconds := int64(ttl.Seconds())
	return zaddCapCompiled.Run(ctx, c.Client, []string{c.Key(id)}, score, member, maxSize, ttlSeconds).Err()
}

// ZIncrBy incrémente le score d'un membre existant.
func (c *Collection) ZIncrBy(ctx context.Context, id any, increment float64, member any) error {
	memberStr := fmt.Sprintf("%v", member)
	return c.Client.ZIncrBy(ctx, c.Key(id), increment, memberStr).Err()
}

// ZRem supprime un ou plusieurs membres d'un ZSET.
func (c *Collection) ZRem(ctx context.Context, id any, members ...any) error {
	return c.Client.ZRem(ctx, c.Key(id), members...).Err()
}

// ZRemRangeByRank supprime les éléments selon leur position (rang) dans le tri.
func (c *Collection) ZRemRangeByRank(ctx context.Context, id any, start, stop int64) error {
	return c.Client.ZRemRangeByRank(ctx, c.Key(id), start, stop).Err()
}

// ZRemRangeByScore supprime les éléments dont le score est compris entre min et max.
func (c *Collection) ZRemRangeByScore(ctx context.Context, id any, min, max string) error {
	return c.Client.ZRemRangeByScore(ctx, c.Key(id), min, max).Err()
}

// ZUnionStore fusionne plusieurs ZSETs de la collection vers un identifiant destination (Cluster-safe).
func (c *Collection) ZUnionStore(ctx context.Context, destID any, sourceIDs []any, aggregate string) error {
	if len(sourceIDs) == 0 {
		return nil
	}
	keys := make([]string, len(sourceIDs))
	for i, src := range sourceIDs {
		keys[i] = c.Key(src)
	}
	if aggregate == "" {
		aggregate = "MAX"
	}
	destKey := c.Key(destID)

	if redisgo.IsClusterClient(c.Client) {
		pipe := c.Client.Pipeline()
		cmds := make([]*redis.ZSliceCmd, len(keys))
		for i, k := range keys {
			cmds[i] = pipe.ZRangeWithScores(ctx, k, 0, -1)
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
			return err
		}
		merged := make(map[string]float64)
		for _, cmd := range cmds {
			zs, errZ := cmd.Result()
			if errZ != nil {
				continue
			}
			for _, z := range zs {
				m := fmt.Sprintf("%v", z.Member)
				prev, exists := merged[m]
				if !exists {
					merged[m] = z.Score
					continue
				}
				switch aggregate {
				case "MIN":
					if z.Score < prev {
						merged[m] = z.Score
					}
				case "SUM":
					merged[m] = prev + z.Score
				default: // MAX
					if z.Score > prev {
						merged[m] = z.Score
					}
				}
			}
		}
		members := make([]*redis.Z, 0, len(merged))
		for m, sc := range merged {
			members = append(members, &redis.Z{Score: sc, Member: m})
		}
		return c.ZSetReplaceAtomic(ctx, destID, members, c.DefaultTTL)
	}

	if err := c.Client.ZUnionStore(ctx, destKey, &redis.ZStore{
		Keys:      keys,
		Aggregate: aggregate,
	}).Err(); err != nil {
		return err
	}
	if c.DefaultTTL > 0 {
		_ = c.Client.Expire(ctx, destKey, c.DefaultTTL).Err()
	}
	return nil
}

// ZRevRange récupère une liste d'éléments triés du plus grand score au plus petit.
func (c *Collection) ZRevRange(ctx context.Context, id any, start, stop int64) ([]string, error) {
	return c.Client.ZRevRange(ctx, c.Key(id), start, stop).Result()
}

// ZRange récupère une liste d'éléments triés du plus petit score au plus grand.
func (c *Collection) ZRange(ctx context.Context, id any, start, stop int64) ([]string, error) {
	return c.Client.ZRange(ctx, c.Key(id), start, stop).Result()
}

// ZRangeByScoreWithLimit extrait les membres dont le score est inférieur ou égal à un seuil maximum (Batching).
func (c *Collection) ZRangeByScoreWithLimit(ctx context.Context, id any, maxScore int64, limit int64) ([]string, error) {
	return c.Client.ZRangeByScore(ctx, c.Key(id), &redis.ZRangeBy{
		Min:    "-inf",
		Max:    strconv.FormatInt(maxScore, 10),
		Offset: 0,
		Count:  limit,
	}).Result()
}

// ZRevRangeByScore extrait les membres triés par score décroissant avec limite.
func (c *Collection) ZRevRangeByScore(ctx context.Context, id any, max string, min string, limit int64) ([]string, error) {
	return c.Client.ZRevRangeByScore(ctx, c.Key(id), &redis.ZRangeBy{
		Max:    max,
		Min:    min,
		Offset: 0,
		Count:  limit,
	}).Result()
}

// ZRangeByScore extrait les membres triés par score croissant avec limite.
func (c *Collection) ZRangeByScore(ctx context.Context, id any, min string, max string, limit int64) ([]string, error) {
	return c.Client.ZRangeByScore(ctx, c.Key(id), &redis.ZRangeBy{
		Min:    min,
		Max:    max,
		Offset: 0,
		Count:  limit,
	}).Result()
}

// ZAddLex ajoute un élément avec un score absolu de 0 pour un tri purement lexicographique.
func (c *Collection) ZAddLex(ctx context.Context, id any, member any) error {
	return c.Client.ZAdd(ctx, c.Key(id), redis.Z{
		Score:  0,
		Member: member,
	}).Err()
}

// ZRangeByLex cherche des éléments par préfixe (Auto-complétion).
func (c *Collection) ZRangeByLex(ctx context.Context, id any, prefix string, offset, limit int64) ([]string, error) {
	if prefix == "" {
		return []string{}, nil
	}
	opt := &redis.ZRangeBy{
		Min:    "[" + prefix,
		Max:    "[" + prefix + "\xff",
		Offset: offset,
		Count:  limit,
	}
	return c.Client.ZRangeByLex(ctx, c.Key(id), opt).Result()
}

// ZScore récupère le score actuel d'un membre.
func (c *Collection) ZScore(ctx context.Context, id any, member any) (float64, error) {
	return c.Client.ZScore(ctx, c.Key(id), fmt.Sprintf("%v", member)).Result()
}

// ZScores abstrait la récupération de plusieurs scores en un seul aller-retour TCP (Pipeline).
// Si un membre n'est pas présent dans le ZSET (redis.Nil), son score vaut 0 sans interrompre le lot.
func (c *Collection) ZScores(ctx context.Context, id any, members []string) ([]float64, error) {
	if len(members) == 0 {
		return nil, nil
	}

	pipe := c.Client.Pipeline()
	for _, member := range members {
		pipe.ZScore(ctx, c.Key(id), member)
	}

	cmds, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}

	scores := make([]float64, len(members))
	for i, cmd := range cmds {
		if fCmd, ok := cmd.(*redis.FloatCmd); ok {
			val, errCmd := fCmd.Result()
			if errCmd != nil {
				if errors.Is(errCmd, redis.Nil) {
					scores[i] = 0
					continue
				}
				return nil, errCmd
			}
			scores[i] = val
		}
	}
	return scores, nil
}

// ZCount compte le nombre d'éléments entre min et max score.
func (c *Collection) ZCount(ctx context.Context, id any, min, max string) (int64, error) {
	return c.Client.ZCount(ctx, c.Key(id), min, max).Result()
}

// ZCard donne la taille totale du set (nombre d'éléments).
func (c *Collection) ZCard(ctx context.Context, id any) (int64, error) {
	return c.Client.ZCard(ctx, c.Key(id)).Result()
}

// ZRevRangeByRanks utilise un Pipeline Redis pour récupérer une liste de rangs spécifiques.
func (c *Collection) ZRevRangeByRanks(ctx context.Context, id any, ranks []int64) ([]string, error) {
	if len(ranks) == 0 {
		return nil, nil
	}

	pipe := c.Client.Pipeline()
	cmds := make([]*redis.StringSliceCmd, 0, len(ranks))
	key := c.Key(id)

	for _, rank := range ranks {
		cmds = append(cmds, pipe.ZRevRange(ctx, key, rank, rank))
	}

	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}

	results := make([]string, 0, len(ranks))
	for _, cmd := range cmds {
		res, err := cmd.Result()
		if err != nil {
			return nil, err
		}
		if len(res) > 0 {
			results = append(results, res[0])
		}
	}
	return results, nil
}

// ZRevRangeWithScores récupère une liste d'éléments triés avec leurs scores respectifs.
func (c *Collection) ZRevRangeWithScores(ctx context.Context, id any, start, stop int64) ([]redis.Z, error) {
	return c.Client.ZRevRangeWithScores(ctx, c.Key(id), start, stop).Result()
}

// ============================================================================
// 3. FONCTIONS GLOBALES (LEGACY)
// ============================================================================

func ZAdd(ctx context.Context, key string, score float64, member interface{}) error {
	return redisgo.CoreClient().ZAdd(ctx, key, redis.Z{Score: score, Member: member}).Err()
}

func ZIncrBy(ctx context.Context, key string, increment float64, member interface{}) error {
	return redisgo.CoreClient().ZIncrBy(ctx, key, increment, fmt.Sprintf("%v", member)).Err()
}

func ZRevRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return redisgo.CoreClient().ZRevRange(ctx, key, start, stop).Result()
}

func ZRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return redisgo.CoreClient().ZRange(ctx, key, start, stop).Result()
}

func ZRemRangeByRank(ctx context.Context, key string, start, stop int64) error {
	return redisgo.CoreClient().ZRemRangeByRank(ctx, key, start, stop).Err()
}

func ZScore(ctx context.Context, key string, member interface{}) (float64, error) {
	return redisgo.CoreClient().ZScore(ctx, key, fmt.Sprintf("%v", member)).Result()
}

func ZCount(ctx context.Context, key, min, max string) (int64, error) {
	return redisgo.CoreClient().ZCount(ctx, key, min, max).Result()
}

func ZCard(ctx context.Context, key string) (int64, error) {
	return redisgo.CoreClient().ZCard(ctx, key).Result()
}

func ZRevRangeByRanks(ctx context.Context, key string, ranks []int64) ([]string, error) {
	if len(ranks) == 0 {
		return nil, nil
	}
	pipe := redisgo.CoreClient().Pipeline()
	cmds := make([]*redis.StringSliceCmd, 0, len(ranks))
	for _, rank := range ranks {
		cmds = append(cmds, pipe.ZRevRange(ctx, key, rank, rank))
	}
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	results := make([]string, 0, len(ranks))
	for _, cmd := range cmds {
		res, err := cmd.Result()
		if err != nil {
			return nil, err
		}
		if len(res) > 0 {
			results = append(results, res[0])
		}
	}
	return results, nil
}

func ZAddWithCap(ctx context.Context, key string, score float64, member any, maxSize int) error {
	return zaddCapCompiled.Run(ctx, redisgo.CoreClient(), []string{key}, score, member, maxSize, 0).Err()
}

func ZRevRangeWithScores(ctx context.Context, key string, start, stop int64) ([]redis.Z, error) {
	return redisgo.CoreClient().ZRevRangeWithScores(ctx, key, start, stop).Result()
}

func Del(ctx context.Context, keys ...string) error {
	return delKeysClusterSafe(ctx, redisgo.CoreClient(), keys)
}

func ZScores(ctx context.Context, key string, members []string) ([]float64, error) {
	if len(members) == 0 {
		return nil, nil
	}
	pipe := redisgo.CoreClient().Pipeline()
	for _, member := range members {
		pipe.ZScore(ctx, key, member)
	}
	cmds, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	scores := make([]float64, len(members))
	for i, cmd := range cmds {
		if fCmd, ok := cmd.(*redis.FloatCmd); ok {
			val, errCmd := fCmd.Result()
			if errCmd != nil {
				if errors.Is(errCmd, redis.Nil) {
					scores[i] = 0
					continue
				}
				return nil, errCmd
			}
			scores[i] = val
		}
	}
	return scores, nil
}

// ZAddMultiple ajoute plusieurs éléments dans des ZSETs distincts (une clé = un ZSET)
// avec le même membre et le même score, en un seul appel réseau via Pipeline.
// Utile pour le système de Sync Ledger (ex: "ajouter le convID à tous les participants").
func (c *Collection) ZAddMultiple(ctx context.Context, ids []int64, score float64, member string) error {
	if len(ids) == 0 {
		return nil
	}

	pipe := c.Client.Pipeline()
	for _, id := range ids {
		key := c.Key(id)
		pipe.ZAdd(ctx, key, redis.Z{
			Score:  score,
			Member: member,
		})
		// Si la collection a un TTL par défaut défini (comme le Ledger), on l'applique
		if c.DefaultTTL > 0 {
			pipe.Expire(ctx, key, c.DefaultTTL)
		}
	}

	_, err := pipe.Exec(ctx)
	return err
}
