package worker

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/metrics"
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

// StartWriteBehindWorkers lance UNIQUEMENT les chemins temps réel à SLA strict et le pool Write-Behind 64 shards :
//   - Time-Decay fenêtre chaude (0..24h), Tendances Hashtags temps réel
//   - Consommateur file Trust Score & Expiration Modération (SLA strict 48h/7j)
//   - Push (FCM) & Email (SMTP)
//   - Pool Write-Behind plastique (1..1024 goroutines sur 64 shards)
//
// Les jobs périodiques lourds (canonicalisation, reco utilisateurs/communautés, warm-up inactifs,
// 4 GC orphelins, trust mensuel, moderation aging, time-decay deep pass) sont orchestrés par
// l'Activity-Aware Job Scheduler (`scheduler_node` en distribué/HA, embarqué en `monolith`).
func StartWriteBehindWorkers(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du moteur Write-Behind temps réel (sans crons périodiques lourds)...")

	// ── ÉTAPE 1 : CHEMINS TEMPS RÉEL À SLA STRICT ───────────────────────────
	// Time-Decay fenêtre chaude (0..24h) uniquement (le deep pass 24h..30j tourne en DEEP_IDLE)
	startScoreUpdaterCron(ctx)

	// Tendances horaires temps réel
	startHashtagTrendCron(ctx)

	// Consommateur de la file Trust Score (événements temps réel)
	startTrustScoreWorker(ctx)

	// Expiration verrous & minuteurs de modération (SLA strict 48h/7j)
	StartModerationExpirationWorker(ctx)

	// ── ÉTAPE 2 : CANAUX EXTERNES TEMPS RÉEL ────────────────────────────────
	startPushNotificationWorker(ctx)
	go startEmailWorker(ctx)

	// ── ÉTAPE 3 : POOL WRITE-BEHIND PLASTIQUE (1..1024 WORKERS / 64 SHARDS) ─
	concurrency := ResolveWorkerConcurrency()
	metrics.SetWorkerConcurrency(concurrency)

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

// StartBackgroundWorkers est conservé comme alias rétrocompatible de StartWriteBehindWorkers.
func StartBackgroundWorkers(ctx context.Context) {
	StartWriteBehindWorkers(ctx)
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
