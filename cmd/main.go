package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/QuentinRegnier/numan-backend/docs"
	"github.com/QuentinRegnier/numan-backend/internal/api"
	"github.com/QuentinRegnier/numan-backend/internal/api/handlers/health_handlers"
	"github.com/QuentinRegnier/numan-backend/internal/api/middleware"
	"github.com/QuentinRegnier/numan-backend/internal/api/websocket"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/minio"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/logger"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/metrics"
	mongogo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	redisgo "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cluster_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/variables_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/QuentinRegnier/numan-backend/internal/worker"
	"github.com/gin-gonic/gin"
)

// @title           Mon API Propre
// @version         1.0
// @description     Documentation de l'API.
// @description Pour mettre à jour :
//
//	go run github.com/swaggo/swag/cmd/swag@latest init -g cmd/main.go -d . --parseDependency --parseInternal
//
// @BasePath        /api/v12
func main() {
	// 1. Initialise le logger avant toute chose
	logger.InitLogger(context.Background())
	// 2. Le 'defer' garantit que même si le serveur crashe (panic),
	// le buffer de logs sera vidé sur le disque avant de mourir.
	defer logger.CloseLogger(context.Background())

	role := ResolveServerRole()
	datacenter := ResolveDatacenterID()

	logger.Log.Info().
		Str("server_role", string(role)).
		Str("datacenter", datacenter).
		Msg("🚀 Démarrage de l'API numan V12")

	// --- INITIALISATION SNOWFLAKE ---

	// 1. On récupère la variable définie dans le docker-compose / StatefulSet
	nodeIDStr := os.Getenv("NODE_ID")
	if nodeIDStr == "" {
		// Par sécurité, si tu oublies de le mettre, on prévient ou on met 0 par défaut
		log.Println("⚠️ ATTENTION : NODE_ID non défini, utilisation de 0 par défaut")
		nodeIDStr = "0"
	}

	// 2. On convertit le string "1" en int64 1
	nodeID, err := pkg.ParseInt64Strict(nodeIDStr)
	if err != nil {
		log.Fatalf("Erreur: NODE_ID doit être un nombre entier. Reçu: %s", nodeIDStr)
	}

	// 3. On lance le moteur Snowflake
	err = pkg.InitSnowflake(nodeID)
	if err != nil {
		log.Fatalf("Impossible d'initialiser Snowflake: %v", err)
	}

	health_handlers.SetNodeIdentity(datacenter, string(role), nodeID)
	log.Printf("✅ Snowflake initialisé avec le Node ID : %d (datacenter=%s, role=%s)", nodeID, datacenter, role)

	// Initialiser PostgreSQL
	postgres.InitPostgres()

	// Initialiser MongoDB
	mongo.InitMongo()

	// Initialiser Redis
	redis.InitRedis()

	// NOUVEAU : Initialiser les collections du Repository Redis
	redisgo.InitCacheDatabase()

	// 🚨 SÉCURITÉ AOF : On ne vide la base au démarrage que si on l'exige explicitement !
	// Sinon, on détruit toutes les requêtes en attente sauvées par l'AOF de Redis.
	if os.Getenv("CLEAN_DB_ON_STARTUP") == "true" {
		log.Println("⚠️ ATTENTION: Nettoyage total des bases de données activé (Mode DEV)")
		service.InitData()
	} else {
		log.Println("💾 Démarrage classique : Conservation des données existantes (AOF Actif)")
	}

	// Initialisation et synchronisation des variables dynamiques depuis Redis (L1)
	variables_service.InitDynamicVariables(context.Background())

	// Initialiser le Hub WebSocket uniquement sur les nœuds qui servent /ws (monolith / ws_node)
	if role.RunsWebSocket() {
		websocket.InitHub()
	}

	// Initialiser MinIO (requis sur tous les rôles : upload REST, activation média WS, cleanup Worker)
	minio.InitMinio()

	// Initialiser la structure MongoDB
	mongogo.InitCacheDatabase()

	// Initialiser le Cuckoo Filter uniquement sur les rôles REST / Worker (inutile sur un ws_node pur)
	if role.NeedsCuckooFilter() {
		service.WarmUpCuckooFilter(context.Background())
	}

	// Enregistrement du callback non-bloquant de calcul initial des recommandations de communautés publiques
	cache_service.RegisterCommunityRecoInitialComputeHook(func(bgCtx context.Context) {
		_, _ = worker.RunCommunityRecommendationCronCycle(bgCtx)
	})

	// --- SMART SEEDING & INDEXATION MONGO (verrou distribué pour éviter toute course au boot multi-pods) ---
	if role.NeedsStartupSeeding() {
		seedCtx := context.Background()
		lockToken, acquired, errLock := redisgo.AcquireCronLock(seedCtx, "startup_seeding", 2*time.Minute)
		if errLock != nil {
			logger.Log.Warn().Err(errLock).Msg("Impossible d'acquérir le verrou distribué startup_seeding ; exécution locale de secours")
			acquired = true
		}
		if acquired {
			func() {
				if lockToken != "" {
					defer func() {
						_ = redisgo.ReleaseCronLock(context.Background(), "startup_seeding", lockToken)
					}()
				}

				count, err := redisgo.RankedPosts.ZCard(seedCtx, variables.StrictRankSubKey(variables.StrictTypeRecent, variables.DefaultContentLanguage))
				if err != nil {
					log.Printf("Erreur: ZCard: %v", err)
				}

				if count == 0 {
					log.Println("⚠️ Cache Redis vide détecté : Lancement du Seeding massif...")
					if err := cache_service.SeedMostCache(seedCtx); err != nil {
						log.Printf("⚠️ Avertissement lors du seeding: %v", err)
					}
					if err := cache_service.SeedGraphCache(seedCtx); err != nil {
						log.Printf("⚠️ Avertissement lors du seeding du graphe: %v", err)
					}

					// ✅ Seeding du profilage allégé et du graphe relationnel restreint
					if err := cache_service.SeedSpeedCache(seedCtx); err != nil {
						log.Printf("⚠️ Avertissement lors du seeding du SPEED cache: %v", err)
					}

					// ✅ Seeding des chronologies utilisateurs pour le profil
					if err := cache_service.SeedUserCache(seedCtx); err != nil {
						log.Printf("⚠️ Avertissement lors du seeding du USER cache: %v", err)
					}
				} else {
					log.Printf("✅ Cache Redis déjà peuplé (%d éléments). Seeding ignoré, démarrage éclair !", count)

					// Amorçage dédié des recommandations de communautés publiques si le ZSET n'existe pas encore
					if commRankedCount, errComm := redisgo.CommunityRecoRanked.ZCard(seedCtx, variables.DefaultContentLanguage); errComm != nil || commRankedCount == 0 {
						if errSeedComm := cache_service.SeedCommunityRecommendations(seedCtx); errSeedComm != nil {
							log.Printf("⚠️ Avertissement lors du seeding des recommandations de communautés: %v", errSeedComm)
						}
					}
				}

				// Amorçage dédié de l'index Redis L1 persistant des articles s'il n'est pas encore initialisé
				if artInit, errInit := redisgo.Exists(seedCtx, variables.RedisKeyArticlesIndexInit); errInit != nil || !artInit {
					if err := cache_service.SeedArticles(seedCtx); err != nil {
						log.Printf("⚠️ Avertissement lors du seeding des articles: %v", err)
					}
				}

				// Initialiser les Index Mongo
				if err := mongo.EnsureIndexes(seedCtx, mongo.MongoClient.Database(variables.MongoDatabaseName)); err != nil {
					log.Fatalf("Erreur lors de la création des index MongoDB: %v", err)
				}
			}()
		} else {
			logger.Log.Info().Str("server_role", string(role)).Msg("⏳ Seeding initial déjà pris en charge par un autre pod du datacenter ; démarrage direct.")
		}
	} else {
		logger.Log.Info().Str("server_role", string(role)).Msg("⚡ Rôle WS_NODE : Cuckoo Filter et Smart Seeding ignorés pour démarrage ultra-rapide.")
	}

	// Contexte dédié aux workers de fond pour contrôler précisément l'ordre d'arrêt sur SIGTERM
	workerCtx, cancelWorkers := context.WithCancel(context.Background())

	// On ne lance les workers internes que si le serveur est en mode monolith ou worker_node
	if role.RunsWorkers() {
		worker.StartBackgroundWorkers(workerCtx)
	} else {
		logger.Log.Info().Str("server_role", string(role)).Msg("Workers de fond désactivés sur cette instance.")
	}

	// Auto-discovery Redis L1 par datacenter (heartbeat périodique + désenregistrement propre sur shutdown)
	var wsStatsProvider func() (int64, int64)
	if role.RunsWebSocket() {
		wsStatsProvider = websocket.HubStats
	}
	stopHeartbeat := cluster_service.StartNodeHeartbeat(context.Background(), cluster_service.NodeHeartbeatConfig{
		Datacenter: datacenter,
		Role:       string(role),
		NodeID:     nodeID,
		PodName:    os.Getenv("POD_NAME"),
	}, wsStatsProvider)

	// On passe de gin.Default() à gin.New() pour retirer les vieux middlewares
	r := gin.New()

	// ✅ NOUVEAU : SÉCURITÉ ANTI-USURPATION D'IP
	errTrust := r.SetTrustedProxies([]string{"127.0.0.1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"})
	if errTrust != nil {
		logger.Log.Warn().Err(errTrust).Msg("Impossible de configurer les TrustedProxies")
	}

	// 1. On branche NOTRE générateur de TraceID en premier
	r.Use(middleware.TraceIDMiddleware())

	// 2. On branche NOTRE Recovery Anti-Crash en second
	r.Use(middleware.CustomRecoveryMiddleware())

	// 3. Métriques Prometheus (compteurs HTTP normalisés par route Gin, sans explosion de cardinalité)
	r.Use(metrics.GinMiddleware())
	if role.RunsWebSocket() {
		metrics.Default().SetWSStatsProvider(websocket.HubStats)
	}
	metrics.Default().SetPostgresStatsProvider(func() (sql.DBStats, bool) {
		db := postgres.WriteDB()
		if db == nil {
			return sql.DBStats{}, false
		}
		return db.Stats(), true
	})
	metrics.Default().SetPostgresReadStatsProvider(func() (sql.DBStats, bool) {
		db := postgres.ReadDB()
		if db == nil {
			return sql.DBStats{}, false
		}
		return db.Stats(), true
	})
	metrics.Default().SetQueueDepthProvider(func(ctx context.Context) (int64, bool) {
		depth, err := redisgo.TotalQueueDepth(ctx)
		if err != nil {
			return 0, false
		}
		return depth, true
	})
	metrics.Default().SetRedisEvictedKeysProvider(redis.CollectEvictedKeys)

	// Sondes Kubernetes actives sur tous les rôles (monolith, api_node, ws_node, worker_node), sans auth ni rate-limit
	r.GET("/healthz", health_handlers.LivenessHandler)
	r.GET("/readyz", health_handlers.ReadinessHandler)

	if role.RunsAPI() {
		api.SetupCommonMiddlewares(r)
	}
	if role.RunsWebSocket() {
		api.SetupWebSocketRoutes(r)
	}
	if role.RunsREST() {
		api.SetupRESTRoutes(r)
		// Initialiser la documentation
		docs.InitDocsRoutes(r)
	}
	if !role.RunsAPI() {
		logger.Log.Info().Str("server_role", string(role)).Msg("Mode WORKER_NODE : Routes API publiques et WebSocket désactivées.")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	metricsPort := strings.TrimSpace(os.Getenv("METRICS_PORT"))
	if metricsPort == "" {
		metricsPort = "9090"
	}

	log.Printf("Server listening on %s (datacenter=%s, role=%s, metrics_port=%s)", port, datacenter, role, metricsPort)
	log.Printf("v12 API ready")

	// SÉCURITÉ : ReadHeaderTimeout court (5s) contre Slowloris, mais Read/Write (60s) adaptés aux uploads mobiles
	httpTimeouts := ResolveHTTPTimeouts()
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadHeaderTimeout: httpTimeouts.ReadHeaderTimeout,
		ReadTimeout:       httpTimeouts.ReadTimeout,
		WriteTimeout:      httpTimeouts.WriteTimeout,
		IdleTimeout:       httpTimeouts.IdleTimeout,
	}

	// Serveur Prometheus interne dédié (port 9090, jamais exposé par l'Ingress Traefik)
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", metrics.Handler())
	metricsSrv := &http.Server{
		Addr:              ":" + metricsPort,
		Handler:           metricsMux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stopSignals()

	go func() {
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Log.Warn().Err(err).Str("metrics_port", metricsPort).Msg("Serveur interne /metrics arrêté avec erreur")
		}
	}()

	srvErrCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErrCh <- err
		}
		close(srvErrCh)
	}()

	select {
	case err := <-srvErrCh:
		if err != nil {
			stopHeartbeat()
			cancelWorkers()
			log.Fatalf("❌ Erreur fatale du serveur HTTP: %v", err)
		}
	case <-sigCtx.Done():
		health_handlers.SetShuttingDown(true)
		stopHeartbeat()
		logger.Log.Info().Str("server_role", string(role)).Str("datacenter", datacenter).
			Msg("🛑 Signal d'arrêt (SIGTERM/SIGINT) reçu : démarrage du Graceful Shutdown...")
	}

	// 1. Drain des requêtes HTTP en cours (25s max) + arrêt du serveur /metrics interne
	httpShutdownCtx, cancelHTTP := context.WithTimeout(context.Background(), 25*time.Second)
	if err := srv.Shutdown(httpShutdownCtx); err != nil {
		logger.Log.Warn().Err(err).Msg("Arrêt forcé du serveur HTTP après expiration du délai")
	}
	_ = metricsSrv.Shutdown(httpShutdownCtx)
	cancelHTTP()

	// 2. Arrêt et vidage propre des 64 workers Write-Behind (60s max pour les lots en cours)
	cancelWorkers()
	if role.RunsWorkers() {
		workerShutdownCtx, cancelWorkerWait := context.WithTimeout(context.Background(), 60*time.Second)
		if err := worker.WaitForShutdown(workerShutdownCtx); err != nil {
			logger.Log.Warn().Err(err).Msg("Délai dépassé lors de l'attente des workers Write-Behind")
		} else {
			logger.Log.Info().Msg("✅ Tous les workers Write-Behind ont terminé leurs lots en cours.")
		}
		cancelWorkerWait()
	}

	// 3. Fermeture propre du Hub WebSocket et des pools de bases de données
	if role.RunsWebSocket() {
		websocket.Shutdown()
	}
	dbShutdownCtx, cancelDB := context.WithTimeout(context.Background(), 10*time.Second)
	if err := mongo.CloseMongo(dbShutdownCtx); err != nil {
		logger.Log.Warn().Err(err).Msg("Erreur lors de la fermeture de MongoDB")
	}
	cancelDB()
	if err := postgres.ClosePostgres(); err != nil {
		logger.Log.Warn().Err(err).Msg("Erreur lors de la fermeture de PostgreSQL")
	}
	if err := redis.CloseRedis(); err != nil {
		logger.Log.Warn().Err(err).Msg("Erreur lors de la fermeture de Redis")
	}

	logger.Log.Info().Msg("✅ Graceful Shutdown terminé proprement.")
}
