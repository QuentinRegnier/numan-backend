package redis

import (
	"context"
	"errors"

	"github.com/QuentinRegnier/numan-backend/internal/variables"
	goredis "github.com/redis/go-redis/v9"
)

// CFExists vérifie l'appartenance d'un élément dans un Cuckoo Filter RedisBloom.
func (c *Collection) CFExists(ctx context.Context, id any, item any) (bool, error) {
	if err := c.checkReady(); err != nil {
		return false, err
	}
	return c.client.Do(ctx, "CF.EXISTS", c.Key(id), item).Bool()
}

// CFAdd insère un élément dans un Cuckoo Filter RedisBloom.
func (c *Collection) CFAdd(ctx context.Context, id any, item any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	return c.client.Do(ctx, "CF.ADD", c.Key(id), item).Err()
}

// CFReserve initialise un Cuckoo Filter dans Redis avec la capacité et les paramètres configurés.
func (c *Collection) CFReserve(ctx context.Context, id any, capacity int) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if capacity <= 0 {
		capacity = variables.UserRecoCuckooCapacity
	}
	key := c.Key(id)
	err := c.client.Do(
		ctx,
		"CF.RESERVE",
		key,
		capacity,
		"BUCKETSIZE",
		variables.UserRecoCuckooBucketSize,
		"MAXITERATIONS",
		variables.UserRecoCuckooMaxIterations,
		"EXPANSION",
		variables.UserRecoCuckooExpansionFactor,
	).Err()
	if err != nil {
		return err
	}
	if c.DefaultTTL > 0 {
		_ = c.client.Expire(ctx, key, c.DefaultTTL).Err()
	}
	return nil
}

// CFAddItems insère un lot d'éléments dans un Cuckoo Filter par lots bornés via Pipeline.
func (c *Collection) CFAddItems(ctx context.Context, id any, items []any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	key := c.Key(id)

	for start := 0; start < len(items); start += defaultBatchChunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + defaultBatchChunkSize
		if end > len(items) {
			end = len(items)
		}
		chunk := items[start:end]

		pipe := c.client.Pipeline()
		for _, it := range chunk {
			pipe.Do(ctx, "CF.ADD", key, it)
		}
		if end == len(items) && c.DefaultTTL > 0 {
			pipe.Expire(ctx, key, c.DefaultTTL)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// CFExistsMany vérifie en lots bornés (Pipeline de CF.EXISTS) quels candidats sont présents dans le Cuckoo Filter.
func (c *Collection) CFExistsMany(ctx context.Context, id any, items []int64) (map[int64]bool, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	res := make(map[int64]bool, len(items))
	if len(items) == 0 {
		return res, nil
	}
	key := c.Key(id)

	for start := 0; start < len(items); start += defaultBatchChunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + defaultBatchChunkSize
		if end > len(items) {
			end = len(items)
		}
		chunk := items[start:end]

		pipe := c.client.Pipeline()
		cmds := make([]*goredis.Cmd, len(chunk))
		for i, item := range chunk {
			cmds[i] = pipe.Do(ctx, "CF.EXISTS", key, item)
		}
		_, err := pipe.Exec(ctx)
		if err != nil && !errors.Is(err, goredis.Nil) {
			return nil, err
		}
		for i, cmd := range cmds {
			exists, errBool := cmd.Bool()
			if errBool == nil && exists {
				res[chunk[i]] = true
			}
		}
	}
	return res, nil
}
