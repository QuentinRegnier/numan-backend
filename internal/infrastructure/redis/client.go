package redis

import (
	"context"
	"os"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/go-redis/redis/v8"
)

var Rdb *redis.Client

// InitRedis initialise la connexion Redis avec un timeout de sécurité
func InitRedis() {
	Rdb = redis.NewClient(&redis.Options{
		Addr:     os.Getenv("REDIS_ADDR"),
		Password: os.Getenv("REDIS_PASSWORD"), // Doit lire le .env / Secret Kubernetes
		DB:       0,
	})

	// Vérifier la connexion avec un timeout de 5 secondes
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := Rdb.Ping(ctx).Err()
	if err != nil {
		numan_log.Fatal(ctx).Err(err).Msg("Impossible de se connecter à Redis")
	}

	numan_log.Info(ctx).Msg("Connexion à Redis réussie")
}

// GetWithTimeout récupère une valeur avec un timeout custom
func GetWithTimeout(key string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return Rdb.Get(ctx, key).Result()
}

// SetWithTimeout ajoute une valeur avec un timeout custom
func SetWithTimeout(key string, value interface{}, expiration time.Duration, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return Rdb.Set(ctx, key, value, expiration).Err()
}
