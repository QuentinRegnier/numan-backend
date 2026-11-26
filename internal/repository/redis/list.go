package redis

import (
	"context"
	"time"

	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
)

func (c *Collection) LPush(ctx context.Context, id any, values ...any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}
	return c.client.LPush(ctx, c.Key(id), values...).Err()
}

func (c *Collection) RPush(ctx context.Context, id any, values ...any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}
	return c.client.RPush(ctx, c.Key(id), values...).Err()
}

func (c *Collection) LPop(ctx context.Context, id any) (string, error) {
	if err := c.checkReady(); err != nil {
		return "", err
	}
	return c.client.LPop(ctx, c.Key(id)).Result()
}

func (c *Collection) RPop(ctx context.Context, id any) (string, error) {
	if err := c.checkReady(); err != nil {
		return "", err
	}
	return c.client.RPop(ctx, c.Key(id)).Result()
}

func (c *Collection) LLen(ctx context.Context, id any) (int64, error) {
	if err := c.checkReady(); err != nil {
		return 0, err
	}
	return c.client.LLen(ctx, c.Key(id)).Result()
}

func (c *Collection) LRange(ctx context.Context, id any, start, stop int64) ([]string, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	return c.client.LRange(ctx, c.Key(id), start, stop).Result()
}

// BLPop dépile de manière bloquante le premier élément de la LIST de la collection.
// Retourne ErrNotFound si le délai expire sans élément disponible.
func (c *Collection) BLPop(ctx context.Context, id any, timeout time.Duration) (string, error) {
	if err := c.checkReady(); err != nil {
		return "", err
	}
	res, err := c.client.BLPop(ctx, timeout, c.Key(id)).Result()
	if err != nil {
		return "", err
	}
	if len(res) < 2 {
		return "", ErrNotFound
	}
	return res[1], nil
}

// LLenOnCore retourne la longueur d'une LIST brute sur Tier 1 Redis Core.
func LLenOnCore(ctx context.Context, fullKey string) (int64, error) {
	core := redisgo.CoreClient()
	if core == nil {
		return 0, nil
	}
	return core.LLen(ctx, fullKey).Result()
}

// LPopOnCore dépile le premier élément d'une LIST brute sur Tier 1 Redis Core.
func LPopOnCore(ctx context.Context, fullKey string) (string, error) {
	core := redisgo.CoreClient()
	if core == nil {
		return "", nil
	}
	return core.LPop(ctx, fullKey).Result()
}

// LPushOnCore empile un ou plusieurs éléments en tête d'une LIST brute sur Tier 1 Redis Core.
func LPushOnCore(ctx context.Context, fullKey string, values ...any) error {
	core := redisgo.CoreClient()
	if core == nil {
		return ErrNotInitialized
	}
	if len(values) == 0 {
		return nil
	}
	return core.LPush(ctx, fullKey, values...).Err()
}

// RPushOnCore empile un ou plusieurs éléments en fin d'une LIST brute sur Tier 1 Redis Core.
func RPushOnCore(ctx context.Context, fullKey string, values ...any) error {
	core := redisgo.CoreClient()
	if core == nil || len(values) == 0 {
		return nil
	}
	return core.RPush(ctx, fullKey, values...).Err()
}

// BLPopOnCore dépile de manière bloquante le premier élément d'une LIST brute sur Tier 1 Redis Core.
// Retourne ErrNotFound si le délai expire sans élément disponible.
func BLPopOnCore(ctx context.Context, fullKey string, timeout time.Duration) (string, error) {
	core := redisgo.CoreClient()
	if core == nil {
		return "", ErrNotInitialized
	}
	res, err := core.BLPop(ctx, timeout, fullKey).Result()
	if err != nil {
		return "", err
	}
	if len(res) < 2 {
		return "", ErrNotFound
	}
	return res[1], nil
}
