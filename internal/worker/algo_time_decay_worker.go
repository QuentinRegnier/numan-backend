package worker

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// scoreJob contient les métriques pré-calculées par la base de données.
// Ce format plat évite le problème de requêtes "N+1" et supprime le besoin d'hydrater
// des objets complets uniquement pour mettre à jour un score mathématique.
type scoreJob struct {
	PostID           int64
	UserID           int64
	LikeCount        int
	CommentCount     int
	ViewCount        int
	HasMedia         bool
	MediaCount       int
	CreatedAt        time.Time
	Hashtags         []string
	IndirectHashtags []string
	Visibility       int
	PriorityLevel    int
	ReportCount      int
	Lang             int
}

// ############################################################################
// # WORKER : TIME-DECAY ENGINE (MOTEUR DE DÉCLIN TEMPOREL)
// ############################################################################

// startScoreUpdaterCron initialise le Worker Pool basé sur le nombre de threads du CPU
// et lance les planificateurs (Tickers) étagés pour actualiser le score algorithmique des posts.
func startScoreUpdaterCron(ctx context.Context) {
	// ── ÉTAPE 1 : INITIALISATION DE LA FILE D'ATTENTE (BUFFER) ──────────────
	jobs := make(chan scoreJob, variables.TimeDecayJobBuffer)

	// Détermination de la limite de concurrence matérielle stricte (Ex: 8 cœurs = 8 workers)
	numWorkers := runtime.GOMAXPROCS(0)
	numan_log.Info(ctx).Int("workers_cpu", numWorkers).Msg("Démarrage du Time-Decay Engine (Calcul des scores)")

	// ── ÉTAPE 2 : LANCEMENT DU POOL DE WORKERS (CONSOMMATEURS) ──────────────
	for i := 0; i < numWorkers; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return // Arrêt gracieux du serveur
				case job := <-jobs:
					mediaCount := job.MediaCount
					if mediaCount == 0 && job.HasMedia {
						mediaCount = 1
					}

					// Mise à jour du score dans le Cache L1 (ZSETs algorithmiques partitionnés par langue)
					cache_service.UpdateScoreWithMetrics(
						ctx,
						job.PostID,
						job.LikeCount,
						job.CommentCount,
						job.ViewCount,
						mediaCount,
						job.CreatedAt,
						job.Hashtags,         // Tags Directs
						job.IndirectHashtags, // Tags Indirects
						job.Visibility,
						job.ReportCount,
						job.PriorityLevel,
						job.Lang,
						int(job.UserID),
					)
				}
			}
		}()
	}

	// ── ÉTAPE 3 : LANCEMENT DES CRONS ÉTAGÉS (PRODUCTEURS) ──────────────────
	// Pour économiser les ressources, on recalcule très souvent les posts récents,
	// et beaucoup moins souvent les posts anciens (qui ont déjà subi un fort déclin).
	go runTierCron(ctx, jobs, 2*time.Minute, "0", "6 hours")          // Tier 1 : Hyperactif
	go runTierCron(ctx, jobs, 15*time.Minute, "6 hours", "24 hours")  // Tier 2 : Actif
	go runTierCron(ctx, jobs, 60*time.Minute, "24 hours", "72 hours") // Tier 3 : Ralenti
	go runTierCron(ctx, jobs, 6*time.Hour, "72 hours", "30 days")     // Tier 4 : Dormant
}

// runTierCron exécute la récupération des métriques sur une tranche d'âge précise.
func runTierCron(ctx context.Context, jobs chan<- scoreJob, interval time.Duration, minAge, maxAge string) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	lockName := fmt.Sprintf("time_decay:%s:%s", minAge, maxAge)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			token, acquired, errLock := redis.AcquireCronLock(ctx, lockName, interval/2)
			if errLock == nil && !acquired && token == "" && redis.CronLocks != nil && redis.CronLocks.Client != nil {
				continue // Une autre instance worker_node exécute déjà ce tier
			}

			// ── FALLBACK L3 STRICT : CONTINUITÉ DE LA DONNÉE ────────────────
			posts, err := postgres.FuncLoadPostsForTimeDecay(ctx, minAge, maxAge)
			if token != "" {
				_ = redis.ReleaseCronLock(ctx, lockName, token)
			}
			if err != nil {
				numan_log.Error(ctx).
					Err(err).
					Str("min_age", minAge).
					Str("max_age", maxAge).
					Msg("Échec critique du Tier Time-Decay : Impossible de lire PostgreSQL (L3)")
				continue
			}

			// ── ENVOI AUX WORKERS POUR CALCUL EN RAM ────────────────────────
			for _, p := range posts {
				jobs <- scoreJob{
					PostID:           p.ID,
					UserID:           p.UserID,
					LikeCount:        p.LikeCount,
					CommentCount:     p.CommentCount,
					ViewCount:        p.ViewCount,
					HasMedia:         p.HasMedia,
					MediaCount:       p.MediaCount,
					CreatedAt:        p.CreatedAt,
					Hashtags:         p.Hashtags,
					IndirectHashtags: p.IndirectHashtags,
					Visibility:       p.Visibility,
					PriorityLevel:    p.PriorityLevel,
					ReportCount:      p.ReportCount,
					Lang:             p.Lang,
				}
			}
		}
	}
}
