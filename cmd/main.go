package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/QuentinRegnier/numan-backend/docs"
	"github.com/QuentinRegnier/numan-backend/internal/api"
	"github.com/QuentinRegnier/numan-backend/internal/api/middleware"
	"github.com/QuentinRegnier/numan-backend/internal/api/websocket"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/minio"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/logger"
	mongogo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	redisgo "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
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

	logger.Log.Info().Msg("🚀 Démarrage de l'API numan V12")

	// --- INITIALISATION SNOWFLAKE ---

	// 1. On récupère la variable définie dans le docker-compose
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

	log.Printf("✅ Snowflake initialisé avec le Node ID : %d", nodeID)

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

	// Initialiser le Hub et lancer sa boucle
	websocket.InitHub()

	// Initiatiser MinIO
	minio.InitMinio()

	// Initiatiser la structure MongoDB
	mongogo.InitCacheDatabase()

	// Initialiser le Cuckoo Filter
	service.WarmUpCuckooFilter(context.Background())

	// --- SMART SEEDING DU MOST CACHE ---
	count, err := redisgo.ZCard(context.Background(), variables.RedisKeyStrictRecent)
	if err != nil {
		log.Printf("Erreur: ZCard: %v", err)
	}

	if count == 0 {
		log.Println("⚠️ Cache Redis vide détecté : Lancement du Seeding massif...")
		if err := cache_service.SeedMostCache(context.Background()); err != nil {
			log.Printf("⚠️ Avertissement lors du seeding: %v", err)
		}
		if err := cache_service.SeedGraphCache(context.Background()); err != nil {
			log.Printf("⚠️ Avertissement lors du seeding du graphe: %v", err)
		}

		// ✅ Seeding du profilage allégé et du graphe relationnel restreint
		if err := cache_service.SeedSpeedCache(context.Background()); err != nil {
			log.Printf("⚠️ Avertissement lors du seeding du SPEED cache: %v", err)
		}

		// ✅ Seeding des chronologies utilisateurs pour le profil
		if err := cache_service.SeedUserCache(context.Background()); err != nil {
			log.Printf("⚠️ Avertissement lors du seeding du USER cache: %v", err)
		}
	} else {
		log.Printf("✅ Cache Redis déjà peuplé (%d éléments). Seeding ignoré, démarrage éclair !", count)
	}

	// Lance le moteur V12
	worker.StartBackgroundWorkers(context.Background())

	// On passe de gin.Default() à gin.New() pour retirer les vieux middlewares
	r := gin.New()

	// ✅ NOUVEAU : SÉCURITÉ ANTI-USURPATION D'IP
	// On indique à Gin de ne lire le X-Forwarded-For QUE s'il vient de nos réseaux privés Docker (Nginx).
	// Tout autre X-Forwarded-For falsifié venant de l'extérieur sera ignoré et Gin utilisera la vraie IP source.
	errTrust := r.SetTrustedProxies([]string{"127.0.0.1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"})
	if errTrust != nil {
		logger.Log.Warn().Err(errTrust).Msg("Impossible de configurer les TrustedProxies")
	}

	// 1. On branche NOTRE générateur de TraceID en premier
	r.Use(middleware.TraceIDMiddleware())

	// 2. On branche NOTRE Recovery Anti-Crash en second
	r.Use(middleware.CustomRecoveryMiddleware())

	api.SetupRoutes(r)

	// Initialiser la documentation
	docs.InitDocsRoutes(r)

	// Initialiser les Index Mongo
	err = mongo.EnsureIndexes(context.Background(), mongo.MongoClient.Database("numan"))
	if err != nil {
		log.Fatalf("Erreur lors de la création des index MongoDB: %v", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server listening on %s", port)
	log.Printf("v12 API ready")

	// SÉCURITÉ : Configuration stricte des Timeouts pour contrer Slowloris
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  5 * time.Second,  // Temps max pour lire la requête (Headers + Body)
		WriteTimeout: 10 * time.Second, // Temps max pour envoyer la réponse
		IdleTimeout:  15 * time.Second, // Temps max de maintien d'une connexion keep-alive
	}

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("❌ Erreur fatale du serveur HTTP: %v", err)
	}
}
