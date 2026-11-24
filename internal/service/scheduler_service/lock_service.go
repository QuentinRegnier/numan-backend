package scheduler_service

import (
	"context"
	"hash/fnv"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	repoPostgres "github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
)

// SQLAdvisoryLockID calcule un identifiant int64 positif déterministe pour util.func_try_scheduler_advisory_lock.
func SQLAdvisoryLockID(jobName string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("numan:scheduler:" + jobName))
	return int64(h.Sum64() & 0x7fffffffffffffff)
}

// AcquireJobLock acquiert le verrou distribué correspondant au LockScope du job (GlobalSQL via Postgres repository, LocalDC via cache_service).
func AcquireJobLock(ctx context.Context, jobName string, scope scheduler_models.LockScope, ttl time.Duration) (release func(context.Context), acquired bool, err error) {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	if scope == scheduler_models.LockScopeGlobalSQL {
		lockID := SQLAdvisoryLockID(jobName)
		rel, ok, hasDB, errPG := repoPostgres.FuncAcquireSchedulerAdvisoryLock(ctx, lockID)
		if errPG != nil {
			return nil, false, errPG
		}
		if hasDB {
			return rel, ok, nil
		}
		// Lorsque WriteDB n'est pas initialisé (ex: tests unitaires Redis-only), repli sur verrou local DC
	}

	token, ok, err := cache_service.AcquireSchedulerLocalLock(ctx, jobName, ttl)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	return func(relCtx context.Context) {
		if relErr := cache_service.ReleaseSchedulerLocalLock(relCtx, jobName, token); relErr != nil {
			numan_log.Warn(relCtx).Err(relErr).Str("job", jobName).Msg("Échec libération verrou Redis scheduler")
		}
	}, true, nil
}
