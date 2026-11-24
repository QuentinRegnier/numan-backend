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

	// ── ÉTAPE 3 : LANCEMENT DES CRONS RAPIDES (FENÊTRE CHAUDE TEMPS RÉEL) ───
	// Les posts hyperactifs (0..24h) restent pilotés en temps réel sur worker_node / monolith.
	// Les posts anciens (24h..30j) sont délégués à RunTimeDecayDeepPass dans l'Activity-Aware Scheduler (DEEP_IDLE).
	go runTierCron(ctx, jobs, 2*time.Minute, "0", "6 hours")         // Tier 1 : Hyperactif
	go runTierCron(ctx, jobs, 15*time.Minute, "6 hours", "24 hours") // Tier 2 : Actif
}

// RunTimeDecayDeepPass recalcule le déclin temporel des posts anciens (24h..72h et 72h..30j)
// en mode coopératif (vérifie ctx.Done() entre chaque post pour préemption <200ms).
func RunTimeDecayDeepPass(ctx context.Context) (processed int64, err error) {
	tiers := [][2]string{
		{"24 hours", "72 hours"},
		{"72 hours", "30 days"},
	}
	for _, tier := range tiers {
		if ctx.Err() != nil {
			return processed, ctx.Err()
		}
		posts, errLoad := postgres.FuncLoadPostsForTimeDecay(ctx, tier[0], tier[1])
		if errLoad != nil {
			numan_log.Warn(ctx).Err(errLoad).Str("min_age", tier[0]).Str("max_age", tier[1]).Msg("Time-Decay Deep Pass : échec lecture L3")
			return processed, errLoad
		}
		for _, p := range posts {
			if ctx.Err() != nil {
				return processed, ctx.Err()
			}
			mediaCount := p.MediaCount
			if mediaCount == 0 && p.HasMedia {
				mediaCount = 1
			}
			cache_service.UpdateScoreWithMetrics(
				ctx,
				p.ID,
				p.LikeCount,
				p.CommentCount,
				p.ViewCount,
				mediaCount,
				p.CreatedAt,
				p.Hashtags,
				p.IndirectHashtags,
				p.Visibility,
				p.ReportCount,
				p.PriorityLevel,
				p.Lang,
				int(p.UserID),
			)
			processed++
		}
	}
	return processed, nil
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
