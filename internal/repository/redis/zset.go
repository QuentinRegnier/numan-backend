package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	goredis "github.com/redis/go-redis/v9"
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

var zaddCapCompiled = goredis.NewScript(zaddCapScript)

// ============================================================================
// 2. MÉTHODES DDD DE LA STRUCTURE "Collection"
// ============================================================================

// ZAdd ajoute un élément avec un score (ou met à jour son score).
func (c *Collection) ZAdd(ctx context.Context, id any, score float64, member any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	return c.client.ZAdd(ctx, c.Key(id), goredis.Z{
		Score:  score,
		Member: member,
	}).Err()
}

// ZAddWithCap insère un élément, plafonne le ZSET et applique le DefaultTTL de la collection en une seule passe atomique via Lua (EVALSHA).
func (c *Collection) ZAddWithCap(ctx context.Context, id any, score float64, member any, maxSize int) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	ttlSeconds := int64(c.DefaultTTL.Seconds())
	return zaddCapCompiled.Run(ctx, c.client, []string{c.Key(id)}, score, member, maxSize, ttlSeconds).Err()
}

// ZAddWithCapAndTTL insère un élément, plafonne le ZSET et applique un TTL explicite en une seule passe atomique via Lua (EVALSHA).
func (c *Collection) ZAddWithCapAndTTL(ctx context.Context, id any, score float64, member any, maxSize int, ttl time.Duration) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	ttlSeconds := int64(ttl.Seconds())
	return zaddCapCompiled.Run(ctx, c.client, []string{c.Key(id)}, score, member, maxSize, ttlSeconds).Err()
}

// ZIncrBy incrémente le score d'un membre existant.
func (c *Collection) ZIncrBy(ctx context.Context, id any, increment float64, member any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	memberStr := fmt.Sprintf("%v", member)
	return c.client.ZIncrBy(ctx, c.Key(id), increment, memberStr).Err()
}

// ZRem supprime un ou plusieurs membres d'un ZSET.
func (c *Collection) ZRem(ctx context.Context, id any, members ...any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(members) == 0 {
		return nil
	}
	return c.client.ZRem(ctx, c.Key(id), members...).Err()
}

// ZRemRangeByRank supprime les éléments selon leur position (rang) dans le tri.
func (c *Collection) ZRemRangeByRank(ctx context.Context, id any, start, stop int64) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	return c.client.ZRemRangeByRank(ctx, c.Key(id), start, stop).Err()
}

// ZRemRangeByScore supprime les éléments dont le score est compris entre min et max.
func (c *Collection) ZRemRangeByScore(ctx context.Context, id any, min, max string) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	return c.client.ZRemRangeByScore(ctx, c.Key(id), min, max).Err()
}

// ZSetReplaceAtomic remplace atomiquement le contenu d'un ZSET (DEL + ZADD + EXPIRE via MULTI/EXEC)
// sans jamais exposer de liste partiellement écrite aux lecteurs concurrents.
func (c *Collection) ZSetReplaceAtomic(ctx context.Context, id any, members []ZMember, ttl time.Duration) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	key := c.Key(id)
	if ttl <= 0 {
		ttl = c.DefaultTTL
	}
	pipe := c.client.TxPipeline()
	pipe.Del(ctx, key)
	if len(members) > 0 {
		zVals := make([]goredis.Z, 0, len(members))
		for _, m := range members {
			zVals = append(zVals, goredis.Z{
				Score:  m.Score,
				Member: m.Member,
			})
		}
		pipe.ZAdd(ctx, key, zVals...)
		if ttl > 0 {
			pipe.Expire(ctx, key, ttl)
		}
	}
	_, err := pipe.Exec(ctx)
	return err
}

// ZUnionStore fusionne plusieurs ZSETs de la collection vers un identifiant destination (Cluster-safe).
func (c *Collection) ZUnionStore(ctx context.Context, destID any, sourceIDs []any, aggregate string) error {
	if err := c.checkReady(); err != nil {
		return err
	}
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

	if redisgo.IsClusterClient(c.client) {
		pipe := c.client.Pipeline()
		cmds := make([]*goredis.ZSliceCmd, len(keys))
		for i, k := range keys {
			cmds[i] = pipe.ZRangeWithScores(ctx, k, 0, -1)
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
			return err
		}
		merged := make(map[string]float64)
		for _, cmd := range cmds {
			zs, errZ := cmd.Result()
			if errZ != nil {
				if errors.Is(errZ, goredis.Nil) {
					continue
				}
				return errZ
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
		members := make([]ZMember, 0, len(merged))
		for m, sc := range merged {
			members = append(members, ZMember{Score: sc, Member: m})
		}
		return c.ZSetReplaceAtomic(ctx, destID, members, c.DefaultTTL)
	}

	if err := c.client.ZUnionStore(ctx, destKey, &goredis.ZStore{
		Keys:      keys,
		Aggregate: aggregate,
	}).Err(); err != nil {
		return err
	}
	if c.DefaultTTL > 0 {
		_ = c.client.Expire(ctx, destKey, c.DefaultTTL).Err()
	}
	return nil
}

// ZRevRange récupère une liste d'éléments triés du plus grand score au plus petit.
func (c *Collection) ZRevRange(ctx context.Context, id any, start, stop int64) ([]string, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	return c.client.ZRevRange(ctx, c.Key(id), start, stop).Result()
}

// ZRange récupère une liste d'éléments triés du plus petit score au plus grand.
func (c *Collection) ZRange(ctx context.Context, id any, start, stop int64) ([]string, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	return c.client.ZRange(ctx, c.Key(id), start, stop).Result()
}

// ZRangeByScoreWithLimit extrait les membres dont le score est inférieur ou égal à un seuil maximum (Batching).
func (c *Collection) ZRangeByScoreWithLimit(ctx context.Context, id any, maxScore int64, limit int64) ([]string, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	return c.client.ZRangeByScore(ctx, c.Key(id), &goredis.ZRangeBy{
		Min:    "-inf",
		Max:    strconv.FormatInt(maxScore, 10),
		Offset: 0,
		Count:  limit,
	}).Result()
}

// ZRevRangeByScore extrait les membres triés par score décroissant avec limite.
func (c *Collection) ZRevRangeByScore(ctx context.Context, id any, max string, min string, limit int64) ([]string, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	return c.client.ZRevRangeByScore(ctx, c.Key(id), &goredis.ZRangeBy{
		Max:    max,
		Min:    min,
		Offset: 0,
		Count:  limit,
	}).Result()
}

// ZRangeByScore extrait les membres triés par score croissant avec limite.
func (c *Collection) ZRangeByScore(ctx context.Context, id any, min string, max string, limit int64) ([]string, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	return c.client.ZRangeByScore(ctx, c.Key(id), &goredis.ZRangeBy{
		Min:    min,
		Max:    max,
		Offset: 0,
		Count:  limit,
	}).Result()
}

// ZAddLex ajoute un élément avec un score absolu de 0 pour un tri purement lexicographique.
func (c *Collection) ZAddLex(ctx context.Context, id any, member any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	return c.client.ZAdd(ctx, c.Key(id), goredis.Z{
		Score:  0,
		Member: member,
	}).Err()
}

// ZRangeByLex cherche des éléments par préfixe (Auto-complétion).
func (c *Collection) ZRangeByLex(ctx context.Context, id any, prefix string, offset, limit int64) ([]string, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	if prefix == "" {
		return []string{}, nil
	}
	opt := &goredis.ZRangeBy{
		Min:    "[" + prefix,
		Max:    "[" + prefix + "\xff",
		Offset: offset,
		Count:  limit,
	}
	return c.client.ZRangeByLex(ctx, c.Key(id), opt).Result()
}

// ZScore récupère le score actuel d'un membre.
func (c *Collection) ZScore(ctx context.Context, id any, member any) (float64, error) {
	if err := c.checkReady(); err != nil {
		return 0, err
	}
	return c.client.ZScore(ctx, c.Key(id), fmt.Sprintf("%v", member)).Result()
}

// ZCount compte le nombre d'éléments entre min et max score.
func (c *Collection) ZCount(ctx context.Context, id any, min, max string) (int64, error) {
	if err := c.checkReady(); err != nil {
		return 0, err
	}
	return c.client.ZCount(ctx, c.Key(id), min, max).Result()
}

// ZCard donne la taille totale du set (nombre d'éléments).
func (c *Collection) ZCard(ctx context.Context, id any) (int64, error) {
	if err := c.checkReady(); err != nil {
		return 0, err
	}
	return c.client.ZCard(ctx, c.Key(id)).Result()
}

// ZRevRangeByRanks utilise un Pipeline Redis pour récupérer une liste de rangs spécifiques.
func (c *Collection) ZRevRangeByRanks(ctx context.Context, id any, ranks []int64) ([]string, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	if len(ranks) == 0 {
		return nil, nil
	}

	pipe := c.client.Pipeline()
	cmds := make([]*goredis.StringSliceCmd, 0, len(ranks))
	key := c.Key(id)

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

// ZRevRangeWithScores récupère une liste d'éléments triés avec leurs scores respectifs (sans fuiter go-redis.Z).
func (c *Collection) ZRevRangeWithScores(ctx context.Context, id any, start, stop int64) ([]ZMember, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	raw, err := c.client.ZRevRangeWithScores(ctx, c.Key(id), start, stop).Result()
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
