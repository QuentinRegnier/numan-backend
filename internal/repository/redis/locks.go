package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	goredis "github.com/redis/go-redis/v9"
)

const releaseLockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
else
    return 0
end
`

var releaseLockCompiled = goredis.NewScript(releaseLockScript)

// AcquireCronLock acquiert un verrou distribué atomique (SETNX) pour un worker Cron et retourne un jeton unique.
func AcquireCronLock(ctx context.Context, jobName string, ttl time.Duration) (string, bool, error) {
	if !CronLocks.IsReady() {
		return "", false, nil
	}
	token := fmt.Sprintf("%d", domain.NowTime().UnixNano())
	acquired, err := CronLocks.SetNX(ctx, jobName, token, ttl)
	if err != nil || !acquired {
		return "", false, err
	}
	return token, true, nil
}

// ReleaseCronLock libère un verrou distribué Cron uniquement si le jeton correspond à l'instance détentrice.
func ReleaseCronLock(ctx context.Context, jobName, token string) error {
	if !CronLocks.IsReady() || token == "" {
		return nil
	}
	return releaseLockCompiled.Run(ctx, CronLocks.client, []string{CronLocks.Key(jobName)}, token).Err()
}

// AcquireRawLockOnCore pose un verrou SETNX sur une clé complète dans Tier 1 Core (repli Tier 2 Object Pool si Core nil).
func AcquireRawLockOnCore(ctx context.Context, fullKey string, ttl time.Duration) (bool, error) {
	client := redisgo.CoreClient()
	if client == nil {
		client = redisgo.ObjectClient()
	}
	if client == nil {
		return false, nil
	}
	return client.SetNX(ctx, fullKey, "1", ttl).Result()
}

// ReleaseRawLockOnCore supprime un verrou posé sur une clé complète dans Tier 1 Core (repli Tier 2 Object Pool si Core nil).
func ReleaseRawLockOnCore(ctx context.Context, fullKey string) error {
	client := redisgo.CoreClient()
	if client == nil {
		client = redisgo.ObjectClient()
	}
	if client == nil {
		return nil
	}
	return client.Del(ctx, fullKey).Err()
}
