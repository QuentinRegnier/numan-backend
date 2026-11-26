package worker

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// runWithDistributedCronLock garantit qu'une seule instance (pod monolith ou worker_node)
// exécute un cycle de cron périodique donné sur un cluster Kubernetes multi-réplicas.
// Retourne true si le cycle a été exécuté, false s'il a été ignoré (verrou déjà détenu ou erreur).
func runWithDistributedCronLock(ctx context.Context, lockKey string, ttl time.Duration, fn func(context.Context)) bool {
	if !redis.CronLocks.IsReady() {
		// Permet aux tests unitaires isolés sans Redis initialisé d'exécuter fn directement.
		fn(ctx)
		return true
	}

	token, acquired, err := redis.AcquireCronLock(ctx, lockKey, ttl)
	if err != nil {
		numan_log.Warn(ctx).Err(err).Str("lock_key", lockKey).Msg("Cron distribué : erreur lors de l'acquisition du verrou Redis")
		return false
	}
	if !acquired {
		return false
	}

	defer func() {
		if token != "" {
			if errRel := redis.ReleaseCronLock(context.Background(), lockKey, token); errRel != nil {
				numan_log.Warn(ctx).Err(errRel).Str("lock_key", lockKey).Msg("Cron distribué : erreur lors de la libération du verrou Redis")
			}
		}
	}()

	fn(ctx)
	return true
}
