package worker

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// ############################################################################
// # MANAGER : CHEF D'ORCHESTRE DE L'ASYNCHRONISME
// ############################################################################

const (
	minWorkerConcurrency     = 1
	defaultWorkerConcurrency = redis.QueueShards // 64 goroutines par défaut (1 par shard)
	maxWorkerConcurrency     = 1024
)

var (
	workerWG    sync.WaitGroup
	workerWGMux sync.Mutex
)

// ResolveWorkerConcurrency résout le nombre plastique de goroutines Write-Behind depuis WORKER_CONCURRENCY.
// Borné entre 1 et 1024 (défaut 64). Permet d'adapter le débit d'un pod worker aux ressources CPU du serveur
// (ex: 64 en monolith, 128/256/512 sur un serveur dédié AMD EPYC) sans jamais modifier les 64 shards Redis.
func ResolveWorkerConcurrency() int {
	raw := strings.TrimSpace(os.Getenv("WORKER_CONCURRENCY"))
	if raw == "" {
		return defaultWorkerConcurrency
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < minWorkerConcurrency {
		return defaultWorkerConcurrency
	}
	if v > maxWorkerConcurrency {
		return maxWorkerConcurrency
	}
	return v
}

// assignWorkerShards répartit équitablement les totalShards Redis (64) entre totalWorkers goroutines.
//   - Si totalWorkers >= totalShards (ex: 64, 128, 256) : chaque worker consomme 1 shard (workerIdx % totalShards),
//     plusieurs goroutines consommant en parallèle chaque shard de façon atomique via BLPOP.
//   - Si totalWorkers < totalShards (ex: 16) : chaque worker couvre un sous-ensemble disjoint de shards via
//     BLPOP multi-clés natif Redis, garantissant qu'aucun des 64 shards ne reste jamais orphelin.
func assignWorkerShards(workerIdx, totalWorkers, totalShards int) []int {
	if totalWorkers <= 0 || totalShards <= 0 {
		return nil
	}
	if totalWorkers >= totalShards {
		return []int{workerIdx % totalShards}
	}
	shards := make([]int, 0, (totalShards/totalWorkers)+1)
	for s := workerIdx; s < totalShards; s += totalWorkers {
		shards = append(shards, s)
	}
	return shards
}

// StartBackgroundWorkers lance tous les processus asynchrones vitaux de l'API numan.
// Il déploie les planificateurs (Crons) et les pools de travailleurs (Sharding plastique) qui
// dépilent les files d'attente Redis H24.
func StartBackgroundWorkers(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du moteur de persistance et des processus asynchrones...")

	// ── ÉTAPE 1 : LANCEMENT DES MOTEURS ALGORITHMIQUES (CRONS) ──────────────

	// Moteur de Time-Decay : Recalcule la chute du score viral au fil du temps
	startScoreUpdaterCron(ctx)

	// Moteur d'Émergence : Fusionne les hashtags similaires (Fautes de frappe)
	startHashtagCanonCron(ctx)

	// Moteur de Tendances : Calcule le classement mondial des hashtags
	startHashtagTrendCron(ctx)

	// Moteur de Warm-up : Régénère en silence les flux des utilisateurs inactifs
	startFeedWarmupCron(ctx)

	// Moteur de Réputation (Trust Score) : Consommateur par lots + Cron mensuel (+0.02)
	startTrustScoreWorker(ctx)
	startMonthlyTrustScoreCron(ctx)

	// Moteur de Modération Unifiée : Vieillissement quadratique M_temps + Expiration verrous & minuteurs
	StartModerationAgingCron(ctx)
	StartModerationExpirationWorker(ctx)

	// Moteur de Recommandation d'Utilisateurs : Rafraîchissement périodique par lots (24-48h)
	startUserRecommendationCron(ctx)

	// Moteur de Recommandation de Communautés Publiques : Recalcul global par langue (2h)
	startCommunityRecommendationCron(ctx)

	// ── ÉTAPE 2 : LANCEMENT DES GARBAGE COLLECTORS (PURGES L3->L2) ──────────

	// Détruit les fichiers physiques et logs des médias orphelins
	startMediaCleanupCron(ctx)

	// Nettoie les Likes qui pointent vers des posts supprimés
	startLikeCleanupCron(ctx)

	// Nettoie les Réactions (Emoji) sur les messages supprimés
	startReactionCleanupCron(ctx)

	// Supprime les sauvegardes de posts qui n'existent plus
	startSavedCleanupCron(ctx)

	// ── ÉTAPE 3 : LANCEMENT DES CANAUX EXTERNES ─────────────────────────────

	// Initialise Firebase Cloud Messaging et consomme les requêtes de Push
	startPushNotificationWorker(ctx)

	// Initialise le client SMTP et consomme les requêtes d'Emails Sortants
	go startEmailWorker(ctx)

	// ── ÉTAPE 4 : DÉPLOIEMENT DU WORKER POOL PLASTIQUE (1..1024 WORKERS / 64 SHARDS) ──
	concurrency := ResolveWorkerConcurrency()

	workerWGMux.Lock()
	for i := 0; i < concurrency; i++ {
		assigned := assignWorkerShards(i, concurrency, redis.QueueShards)
		workerWG.Add(1)

		go func(workerID int, shards []int) {
			defer workerWG.Done()
			runWorkerOnShards(ctx, workerID, shards)
		}(i, assigned)
	}
	workerWGMux.Unlock()

	numan_log.Info(ctx).
		Int("worker_concurrency", concurrency).
		Int("queue_shards", redis.QueueShards).
		Msg("Moteur de persistance asynchrone opérationnel (pool Write-Behind plastique à l'écoute).")
}

// WaitForShutdown attend la fin propre de tous les workers Write-Behind actifs jusqu'à expiration du contexte.
func WaitForShutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		workerWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
