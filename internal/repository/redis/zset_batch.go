package redis

import (
	"context"
	"errors"

	goredis "github.com/redis/go-redis/v9"
)

// ZRevRangeBatch exécute ZREVRANGE [start, stop] en pipeline sur plusieurs clés de la collection (Cluster-safe, chunké).
// Retourne une tranche alignée positionnellement 1:1 avec `ids`.
func (c *Collection) ZRevRangeBatch(ctx context.Context, ids []any, start, stop int64) ([][]string, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	out := make([][]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	for offset := 0; offset < len(ids); offset += defaultBatchChunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := offset + defaultBatchChunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[offset:end]

		pipe := c.client.Pipeline()
		cmds := make([]*goredis.StringSliceCmd, len(chunk))
		for i, id := range chunk {
			cmds[i] = pipe.ZRevRange(ctx, c.Key(id), start, stop)
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
			out[offset+i] = vals
		}
	}
	return out, nil
}

// ZScores abstrait la récupération de plusieurs scores en un seul aller-retour TCP (Pipeline).
// Si un membre n'est pas présent dans le ZSET (ErrNotFound), son score vaut 0 sans interrompre le lot.
func (c *Collection) ZScores(ctx context.Context, id any, members []string) ([]float64, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, nil
	}

	key := c.Key(id)
	scores := make([]float64, len(members))

	for start := 0; start < len(members); start += defaultBatchChunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + defaultBatchChunkSize
		if end > len(members) {
			end = len(members)
		}
		chunk := members[start:end]

		pipe := c.client.Pipeline()
		cmds := make([]*goredis.FloatCmd, len(chunk))
		for i, member := range chunk {
			cmds[i] = pipe.ZScore(ctx, key, member)
		}

		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
			return nil, err
		}

		for i, cmd := range cmds {
			val, errCmd := cmd.Result()
			if errCmd != nil {
				if errors.Is(errCmd, goredis.Nil) {
					scores[start+i] = 0
					continue
				}
				return nil, errCmd
			}
			scores[start+i] = val
		}
	}
	return scores, nil
}

// ZAddMultiple ajoute plusieurs éléments dans des ZSETs distincts (une clé = un ZSET)
// avec le même membre et le même score, en lots bornés via Pipeline (Cluster-safe).
func (c *Collection) ZAddMultiple(ctx context.Context, ids []int64, score float64, member string) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}

	for start := 0; start < len(ids); start += defaultBatchChunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + defaultBatchChunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]

		pipe := c.client.Pipeline()
		for _, id := range chunk {
			key := c.Key(id)
			pipe.ZAdd(ctx, key, goredis.Z{
				Score:  score,
				Member: member,
			})
			if c.DefaultTTL > 0 {
				pipe.Expire(ctx, key, c.DefaultTTL)
			}
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ZAddAndTrimBatch ajoute un membre scoré dans un lot de ZSETs puis tronque chacun via ZREMRANGEBYRANK 0 trimStop.
// Utilisé notamment pour le fan-out des boîtes aux lettres (Cluster-safe, lots bornés).
func (c *Collection) ZAddAndTrimBatch(ctx context.Context, ids []int64, score float64, member any, trimStop int64) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}

	for start := 0; start < len(ids); start += defaultBatchChunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + defaultBatchChunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]

		pipe := c.client.Pipeline()
		for _, id := range chunk {
			key := c.Key(id)
			pipe.Do(ctx, "ZADD", key, score, member)
			pipe.Do(ctx, "ZREMRANGEBYRANK", key, 0, trimStop)
			if c.DefaultTTL > 0 {
				pipe.Expire(ctx, key, c.DefaultTTL)
			}
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}
