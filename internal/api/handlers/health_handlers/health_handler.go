package health_handlers

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

const readinessCheckTimeout = 1500 * time.Millisecond

// NodeIdentity identifie l'instance courante (datacenter, rôle serveur, Node ID Snowflake).
type NodeIdentity struct {
	Datacenter string
	Role       string
	NodeID     int64
}

var (
	shuttingDown atomic.Bool
	identityMu   sync.RWMutex
	nodeIdentity NodeIdentity
)

// SetNodeIdentity enregistre l'identité opérationnelle du nœud exposée sur /healthz et /readyz.
func SetNodeIdentity(datacenter, role string, nodeID int64) {
	identityMu.Lock()
	nodeIdentity = NodeIdentity{
		Datacenter: datacenter,
		Role:       role,
		NodeID:     nodeID,
	}
	identityMu.Unlock()
}

func currentNodeIdentity() NodeIdentity {
	identityMu.RLock()
	id := nodeIdentity
	identityMu.RUnlock()
	return id
}

func attachNodeIdentity(payload gin.H) gin.H {
	id := currentNodeIdentity()
	if id.Role != "" {
		payload["role"] = id.Role
		payload["datacenter"] = id.Datacenter
		payload["node_id"] = id.NodeID
	}
	return payload
}

// SetShuttingDown bascule l'état de drainage du pod (fait répondre 503 sur /readyz).
func SetShuttingDown(v bool) {
	shuttingDown.Store(v)
}

// IsShuttingDown indique si le pod est en cours d'arrêt gracieux.
func IsShuttingDown() bool {
	return shuttingDown.Load()
}

// ReadinessChecker injecte les vérifications de connectivité L1/L2/L3 (facilitant les tests unitaires).
type ReadinessChecker struct {
	CheckRedis        func(ctx context.Context) error
	CheckPostgres     func(ctx context.Context) error
	CheckPostgresRead func(ctx context.Context) error
	CheckMongo        func(ctx context.Context) error
}

// DefaultReadinessChecker vérifie les clients d'infrastructure réels (Redis L1, Mongo L2, Postgres Primary + Read Replica L3).
func DefaultReadinessChecker() ReadinessChecker {
	checker := ReadinessChecker{
		CheckRedis: func(ctx context.Context) error {
			if redis.Rdb == nil {
				return errors.New("redis client not initialized")
			}
			return redis.Rdb.Ping(ctx).Err()
		},
		CheckPostgres: func(ctx context.Context) error {
			db := postgres.WriteDB()
			if db == nil {
				return errors.New("postgres primary client not initialized")
			}
			return db.PingContext(ctx)
		},
		CheckMongo: func(ctx context.Context) error {
			if mongo.MongoClient == nil {
				return errors.New("mongo client not initialized")
			}
			// PrimaryPreferred évite d'évincer les pods API pendant une brève élection Raft (<2s)
			return mongo.MongoClient.Ping(ctx, readpref.PrimaryPreferred())
		},
	}

	if postgres.HasDedicatedReadPool() {
		checker.CheckPostgresRead = func(ctx context.Context) error {
			return postgres.ReadDB().PingContext(ctx)
		}
	}

	return checker
}

// LivenessHandler répond 200 tant que le processus Go est vivant (sans dépendre des BDD externes).
func LivenessHandler(c *gin.Context) {
	c.JSON(http.StatusOK, attachNodeIdentity(gin.H{"status": "alive"}))
}

// ReadinessHandler utilise le checker par défaut sur Redis/PostgreSQL/MongoDB.
func ReadinessHandler(c *gin.Context) {
	NewReadinessHandler(DefaultReadinessChecker())(c)
}

// NewReadinessHandler construit un handler /readyz vérifiant l'état de shutdown et les 3 couches de stockage.
// Le pool postgres_read est vérifié en mode dégradé toléré : un échec du Read Replica ne fait pas tomber le pod en 503
// tant que le Primary PostgreSQL reste joignable.
func NewReadinessHandler(checker ReadinessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsShuttingDown() {
			c.JSON(http.StatusServiceUnavailable, attachNodeIdentity(gin.H{"status": "shutting_down"}))
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), readinessCheckTimeout)
		defer cancel()

		checks := map[string]string{
			"redis":    "ok",
			"postgres": "ok",
			"mongo":    "ok",
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
						ready = false
					} else {
						checks[name] = "degraded"
					}
					mu.Unlock()
				}
			}()
		}

		runCheck("redis", checker.CheckRedis, true)
		runCheck("postgres", checker.CheckPostgres, true)
		runCheck("postgres_read", checker.CheckPostgresRead, false)
		runCheck("mongo", checker.CheckMongo, true)
		wg.Wait()

		if !ready {
			c.JSON(http.StatusServiceUnavailable, attachNodeIdentity(gin.H{
				"status": "not_ready",
				"checks": checks,
			}))
			return
		}

		c.JSON(http.StatusOK, attachNodeIdentity(gin.H{
			"status": "ready",
			"checks": checks,
		}))
	}
}
