package redis

import (
	"context"
	"fmt"
	"time"

	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	goredis "github.com/redis/go-redis/v9"
)

// Collection encapsule un préfixe logique, son client Redis universel (privé) et son TTL par défaut.
// Aucun appelant externe ne peut accéder au client go-redis sous-jacent.
type Collection struct {
	Prefix     string // ex: "post_service" (donnera "post_service:123")
	client     goredis.UniversalClient
	DefaultTTL time.Duration
}

func newCoreCollection(prefix string, ttl time.Duration) *Collection {
	return &Collection{
		Prefix:     prefix,
		client:     redisgo.CoreClient(),
		DefaultTTL: ttl,
	}
}

func newObjectCollection(prefix string, ttl time.Duration) *Collection {
	return &Collection{
		Prefix:     prefix,
		client:     redisgo.ObjectClient(),
		DefaultTTL: ttl,
	}
}

func newPubSubCollection(prefix string, ttl time.Duration) *Collection {
	return &Collection{
		Prefix:     prefix,
		client:     redisgo.PubSubClient(),
		DefaultTTL: ttl,
	}
}

// IsReady indique si la collection est initialisée et adossée à un client Redis actif.
func (c *Collection) IsReady() bool {
	return c != nil && c.client != nil
}

func (c *Collection) checkReady() error {
	if !c.IsReady() {
		return ErrNotInitialized
	}
	return nil
}

// Key gère la construction stricte de la clé Redis finale : "prefix:id".
func (c *Collection) Key(id any) string {
	return fmt.Sprintf("%s:%v", c.Prefix, id)
}

// Exists vérifie rapidement si la clé d'une collection existe (O(1)).
func (c *Collection) Exists(ctx context.Context, id any) (bool, error) {
	if err := c.checkReady(); err != nil {
		return false, err
	}
	count, err := c.client.Exists(ctx, c.Key(id)).Result()
	return count > 0, err
}

// DeleteObject supprime un objet du cache.
func (c *Collection) DeleteObject(ctx context.Context, id any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	return c.client.Del(ctx, c.Key(id)).Err()
}

// DeletePrimitive supprime une valeur primitive brute.
func (c *Collection) DeletePrimitive(ctx context.Context, id any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	return c.client.Del(ctx, c.Key(id)).Err()
}

// Unlink supprime de manière non-bloquante (asynchrone côté Redis) une clé de la collection.
func (c *Collection) Unlink(ctx context.Context, id any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	return c.client.Unlink(ctx, c.Key(id)).Err()
}

// RefreshTTL prolonge la durée de vie d'un objet (utile pour les sessions).
func (c *Collection) RefreshTTL(ctx context.Context, id any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	return c.client.Expire(ctx, c.Key(id), c.DefaultTTL).Err()
}

// Expire applique un TTL explicite sur une clé de la collection.
func (c *Collection) Expire(ctx context.Context, id any, ttl time.Duration) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = c.DefaultTTL
	}
	if ttl <= 0 {
		return nil
	}
	return c.client.Expire(ctx, c.Key(id), ttl).Err()
}

// ExpireNX applique un TTL uniquement si la clé ne possède pas encore d'expiration (Redis 7+ EXPIRE ... NX).
func (c *Collection) ExpireNX(ctx context.Context, id any, ttl time.Duration) (bool, error) {
	if err := c.checkReady(); err != nil {
		return false, err
	}
	if ttl <= 0 {
		ttl = c.DefaultTTL
	}
	if ttl <= 0 {
		return false, nil
	}
	seconds := int64(ttl / time.Second)
	if seconds <= 0 {
		seconds = 1
	}
	ok, err := c.client.Do(ctx, "EXPIRE", c.Key(id), seconds, "NX").Bool()
	if err != nil {
		// Compatibilité descendante si Redis < 7 rejette l'option NX
		return c.client.Expire(ctx, c.Key(id), ttl).Result()
	}
	return ok, nil
}

// TTL retourne la durée de vie restante d'une clé de la collection (-1 si persistante, -2 si absente).
func (c *Collection) TTL(ctx context.Context, id any) (time.Duration, error) {
	if err := c.checkReady(); err != nil {
		return 0, err
	}
	return c.client.TTL(ctx, c.Key(id)).Result()
}
