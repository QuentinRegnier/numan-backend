package health_service

import (
	"context"
	"sync"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/health_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	repoMongo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	repoPostgres "github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
)

const readinessCheckTimeout = 1500 * time.Millisecond

// ReadinessChecker injecte les vérifications de connectivité L1/L2/L3 (facilitant les tests unitaires).
type ReadinessChecker struct {
	CheckRedis        func(ctx context.Context) error
	CheckRedisObjects func(ctx context.Context) error
	CheckRedisPubSub  func(ctx context.Context) error
	RedisPubSubCrit   bool
	CheckPostgres     func(ctx context.Context) error
	CheckPostgresRead func(ctx context.Context) error
	CheckMongo        func(ctx context.Context) error
}

// DefaultReadinessChecker construit le vérificateur standard via cache_service et les repositories L2/L3.
func DefaultReadinessChecker() ReadinessChecker {
	id := CurrentNodeIdentity()
	pubsubCritical := id.Role == "" || id.Role == "monolith" || id.Role == "ws_node"

	checker := ReadinessChecker{
		CheckRedis:      cache_service.PingRedisCore,
		RedisPubSubCrit: pubsubCritical,
		CheckPostgres:   repoPostgres.FuncPingWrite,
		CheckMongo:      repoMongo.FuncPingMongo,
	}

	if cache_service.HasDedicatedObjectPool() {
		checker.CheckRedisObjects = cache_service.PingRedisObjects
	}
	if cache_service.HasDedicatedPubSub() {
		checker.CheckRedisPubSub = cache_service.PingRedisPubSub
	}
	if repoPostgres.FuncHasDedicatedReadPool() {
		checker.CheckPostgresRead = repoPostgres.FuncPingRead
	}
	return checker
}

// CheckReadiness vérifie l'état de drainage et les 3 couches de stockage L1/L2/L3.
// Retourne le payload typé et un booléen indiquant si le pod est prêt (HTTP 200 vs 503).
//   - redis / redis_core : CRITIQUE (503 en cas d'échec)
//   - redis_objects (Tier 2 non-répliqué) : DÉGRADÉ TOLÉRÉ (200 OK avec "degraded", repli MongoDB L2)
//   - redis_pubsub (Tier 3) : CRITIQUE sur ws_node/monolith, DÉGRADÉ TOLÉRÉ sur api_node/worker_node/scheduler_node
//   - postgres_read : DÉGRADÉ TOLÉRÉ tant que Primary reste joignable
func CheckReadiness(parentCtx context.Context, checker ReadinessChecker) (health_models.ReadinessOutput, bool) {
	id := CurrentNodeIdentity()
	out := health_models.ReadinessOutput{}
	if id.Role != "" {
		out.Role = id.Role
		out.Datacenter = id.Datacenter
		out.NodeID = id.NodeID
	}

	if IsShuttingDown() {
		out.Status = "shutting_down"
		return out, false
	}

	ctx, cancel := context.WithTimeout(parentCtx, readinessCheckTimeout)
	defer cancel()

	checks := map[string]string{
		"redis":      "ok",
		"redis_core": "ok",
		"postgres":   "ok",
		"mongo":      "ok",
	}
	if checker.CheckRedisObjects != nil {
		checks["redis_objects"] = "ok"
	}
	if checker.CheckRedisPubSub != nil {
		checks["redis_pubsub"] = "ok"
	}
	if checker.CheckPostgresRead != nil {
		checks["postgres_read"] = "ok"
	}

	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		ready = true
	)

	runCheck := func(name string, fn func(context.Context) error, critical bool) {
		if fn == nil {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(ctx); err != nil {
				numan_log.Warn(ctx).Err(err).Str("dependency", name).Bool("critical", critical).Msg("Readiness check failed")
				mu.Lock()
				if critical {
					checks[name] = "error"
					if name == "redis" {
						checks["redis_core"] = "error"
					}
					ready = false
				} else {
					checks[name] = "degraded"
				}
				mu.Unlock()
			}
		}()
	}

	pubsubCritical := checker.RedisPubSubCrit || id.Role == "" || id.Role == "monolith" || id.Role == "ws_node"

	runCheck("redis", checker.CheckRedis, true)
	runCheck("redis_objects", checker.CheckRedisObjects, false)
	runCheck("redis_pubsub", checker.CheckRedisPubSub, pubsubCritical)
	runCheck("postgres", checker.CheckPostgres, true)
	runCheck("postgres_read", checker.CheckPostgresRead, false)
	runCheck("mongo", checker.CheckMongo, true)
	wg.Wait()

	out.Checks = checks
	if !ready {
		out.Status = "not_ready"
		return out, false
	}
	out.Status = "ready"
	return out, true
}
