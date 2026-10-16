package worker

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/feed_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/algorithm_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/feed_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # WORKER : FEED WARM-UP & SOCIAL FAN-OUT
// ############################################################################

// startFeedWarmupCron orchestre l'auto-génération des flux d'actualités par lots pour les utilisateurs inactifs.
// S'exécute à intervalles réguliers sans jamais scanner l'intégralité de la BDD (O(log(N) + M)).
func startFeedWarmupCron(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du Moteur de Warm-up Algorithmique (Feed)...")

	go func() {
		ticker := time.NewTicker(variables.FeedWarmupCronInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				processScheduledWarmups(ctx)
			}
		}
	}()
}

// processScheduledWarmups identifie et régénère les flux des utilisateurs dont le cache L1 a expiré.
func processScheduledWarmups(ctx context.Context) {
	now := time.Now().Unix()

	// ── ÉTAPE 1 : EXTRACTION CHIRURGICALE DES UTILISATEURS (L1) ─────────────
	expiredUserIDs, err := redis.FeedSchedule.ZRangeByScoreWithLimit(ctx, "global", now, variables.FeedWarmupBatchSize)
	if err != nil || len(expiredUserIDs) == 0 {
		return // Personne n'est arrivé à échéance
	}

	numan_log.Info(ctx).Int("count", len(expiredUserIDs)).Msg("Warm-up Feed : Traitement d'un lot d'utilisateurs éligibles.") // ── ÉTAPE 2 : ANALYSE DU NIVEAU D'INACTIVITÉ (TÉLÉMÉTRIE L1) ────────────
	for _, idStr := range expiredUserIDs {
		userID := pkg.ParseInt64(idStr)
		if userID == 0 {
			continue
		}

		lastActiveAt, isOnline := getTelemetryData(ctx, userID)

		// Coupe-circuit : Si l'utilisateur est en ligne, le Feed est géré en direct.
		if isOnline {
			_ = redis.FeedSchedule.ZAdd(ctx, "global", float64(time.Now().Add(1*time.Hour).Unix()), userID)
			continue
		}

		inactivityDuration := time.Since(lastActiveAt)

		// ── ÉTAPE 3 : LOI DE DÉCROISSANCE EXPONENTIELLE DU CALCUL (§4.4) ─────
		if inactivityDuration < 2*24*time.Hour {
			// NIVEAU 1 : Inactivité récente (< 2 jours) -> Régénération soutenue (12h)
			executeBackgroundGeneration(ctx, userID)
			nextRun := time.Now().Add(12 * time.Hour).Unix()
			_ = redis.FeedSchedule.ZAdd(ctx, "global", float64(nextRun), userID)

		} else if inactivityDuration >= 2*24*time.Hour && inactivityDuration < 7*24*time.Hour {
			// NIVEAU 2 : Absent temporaire (2-7 jours) -> Régénération dégradée (48h)
			executeBackgroundGeneration(ctx, userID)
			nextRun := time.Now().Add(48 * time.Hour).Unix()
			_ = redis.FeedSchedule.ZAdd(ctx, "global", float64(nextRun), userID)

		} else {
			// NIVEAU 3 : Mode Dormant (>= 7 jours) -> Éviction absolue
			// Protection RAM : L'utilisateur a abandonné l'app, on libère l'espace.
			numan_log.Info(ctx).Int64("user_id", userID).Msg("Warm-up Feed : Utilisateur classé DORMANT. Éviction de la RAM L1 en cours.")

			_ = algorithm_service.DeleteUserFeedState(ctx, userID)
			_ = redis.FeedSchedule.ZRem(ctx, "global", userID)
		}
	}
}

// executeBackgroundGeneration simule une requête API interne pour forcer la régénération algorithmique.
func executeBackgroundGeneration(ctx context.Context, userID int64) {
	numan_log.Info(ctx).Int64("user_id", userID).Msg("Warm-up Feed : Pré-calcul d'un flux frais (Background).")

	input := feed_models.GetFeedInput{
		Force:         true, // Purge le Cuckoo Filter pour brasser de nouveaux contenus
		LastSeenIndex: 0,
	}

	// Appel transparent au Service Unifié
	_, _, _, _ = feed_service.GetFeed(ctx, userID, input)
}

// getTelemetryData interroge les caches L1 pour déterminer le statut d'activité d'un profil.
func getTelemetryData(ctx context.Context, userID int64) (time.Time, bool) {
	isOnline := cache_service.IsUserOnline(ctx, userID)
	var lastActiveAt time.Time

	profile, err := cache_service.GetTelemetryProfile(ctx, userID)
	if err == nil && profile.TimestampMs > 0 {
		tsMs := profile.TimestampMs
		lastActiveAt = time.UnixMilli(tsMs)
	} else {
		// En l'absence totale de télémétrie, on simule une inactivité lointaine pour déclencher le Mode Dormant.
		lastActiveAt = time.Now().Add(-30 * 24 * time.Hour)
	}

	return lastActiveAt, isOnline
}

// handleSocialFanOut intercepte les créations de posts pour distribuer l'ID
// dans les boîtes aux lettres Redis (Mailboxes) du graphe social de l'auteur.
func handleSocialFanOut(ctx context.Context, events []redis.AsyncEvent) {
	for _, evt := range events {
		if evt.Type == redis.EntityPost && evt.Action == redis.ActionCreate {
			postID := evt.ID

			// ── ÉTAPE 1 : VÉRIFICATION ABSOLUE VIA FALLBACK (SÉCURITÉ) ──────
			p, errFallback := getPostWithFallback(ctx, postID)
			if errFallback != nil || p.Visibility == -1 {
				continue // Post introuvable ou supprimé dans l'intervalle
			}

			authorID := p.UserID
			var targetIDs []int64
			var errGraph error

			// ── ÉTAPE 2 : FILTRE DE VISIBILITÉ ET ROUTAGE HYBRIDE ───────────
			if p.Visibility == variables.PostVisibilityFriend {
				// POST AMIS : Graphe Bidirectionnel Strict (Amis Uniquement)
				targetIDs, errGraph = cache_service.GetSpeedFriends(ctx, authorID)
			} else if p.Visibility == variables.PostVisibilityAll || p.Visibility == variables.PostVisibilitySubcriber {
				// POST PUBLIC/ABONNÉS : Graphe Unidirectionnel
				// Coupe-Circuit : Empêcher le blocage RAM pour les comptes hyper-suivis
				followerCount := cache_service.GetFollowerCount(ctx, authorID)
				if followerCount > variables.FanOutVIPThreshold {
					numan_log.Info(ctx).
						Int64("author_id", authorID).
						Int64("follower_count", followerCount).
						Msg("FanOut annulé pour profil VIP (Justin Bieber Effect). Délégation au Most Cache.")
					continue
				}

				targetIDs, errGraph = cache_service.GetSpeedRelationsIndex(ctx, authorID)
			} else {
				continue // Visibilité non supportée
			}

			if errGraph != nil {
				numan_log.Warn(ctx).Err(errGraph).Int64("author_id", authorID).Msg("FanOut : Impossible de résoudre le graphe social.")
				continue
			}

			if len(targetIDs) == 0 {
				continue // Aucune audience, annulation du FanOut
			}

			// ── ÉTAPE 3 : DISTRIBUTION DE MASSE VIA PIPELINE REDIS ──────────
			pipe := redis.FeedsMailbox.Pipeline()
			score := float64(time.Now().UnixMilli())

			for _, followerID := range targetIDs {
				mailboxKey := redis.FeedsMailbox.Key(followerID)

				// Injection du PostID au sommet chronologique
				pipe.Do(ctx, "ZADD", mailboxKey, score, postID)

				// Purge glissante (Capacité stricte à 500 posts par mailbox)
				pipe.Do(ctx, "ZREMRANGEBYRANK", mailboxKey, 0, variables.FanOutRetentionLimit)
			}

			_, errPipe := pipe.Exec(ctx)
			if errPipe != nil {
				numan_log.Error(ctx).Err(errPipe).Int64("post_id", postID).Msg("Échec de l'exécution du pipeline de distribution FanOut.")
			}
		}
	}
}
