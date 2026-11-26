package worker

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
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

// processScheduledWarmups identifie et régénère les flux des utilisateurs dont le cache L1 a expiré.
func processScheduledWarmups(ctx context.Context) {
	lockToken, acquired, errLock := redis.AcquireCronLock(ctx, "cron:feed_warmup", variables.FeedWarmupCronInterval)
	if errLock != nil || !acquired {
		return
	}
	defer func() {
		_ = redis.ReleaseCronLock(ctx, "cron:feed_warmup", lockToken)
	}()

	now := domain.TimeToMillis(domain.NowTime())

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
			_ = redis.FeedSchedule.ZAdd(ctx, "global", float64(domain.TimeToMillis(domain.NowTime().Add(1*time.Hour))), userID)
			continue
		}

		inactivityDuration := time.Since(lastActiveAt)

		// ── ÉTAPE 3 : LOI DE DÉCROISSANCE EXPONENTIELLE DU CALCUL (§4.4) ─────
		if inactivityDuration < 2*24*time.Hour {
			// NIVEAU 1 : Inactivité récente (< 2 jours) -> Régénération soutenue (12h)
			executeBackgroundGeneration(ctx, userID)
			nextRun := domain.TimeToMillis(domain.NowTime().Add(12 * time.Hour))
			_ = redis.FeedSchedule.ZAdd(ctx, "global", float64(nextRun), userID)

		} else if inactivityDuration >= 2*24*time.Hour && inactivityDuration < 7*24*time.Hour {
			// NIVEAU 2 : Absent temporaire (2-7 jours) -> Régénération dégradée (48h)
			executeBackgroundGeneration(ctx, userID)
			nextRun := domain.TimeToMillis(domain.NowTime().Add(48 * time.Hour))
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
		lastActiveAt = domain.MillisToTime(tsMs)
	} else {
		// En l'absence totale de télémétrie, on simule une inactivité lointaine pour déclencher le Mode Dormant.
		lastActiveAt = domain.NowTime().Add(-30 * 24 * time.Hour)
	}

	return lastActiveAt, isOnline
}

// formatMailboxMember encode le membre stocké dans FeedsMailbox sous la forme "{postID}:{authorID}"
// afin de permettre le filtrage par auteur en O(1) à la lecture du panier sans requête réseau supplémentaire.
func formatMailboxMember(postID, authorID int64) string {
	if authorID > 0 {
		return fmt.Sprintf("%d:%d", postID, authorID)
	}
	return strconv.FormatInt(postID, 10)
}

// isAuthorFanOutThrottled vérifie si un auteur a déjà atteint son quota quotidien de Fan-Out
// sur une fenêtre glissante de 24h (variables.FanOutMaxPostsPerAuthorPerDay).
func isAuthorFanOutThrottled(ctx context.Context, authorID, postID int64) bool {
	maxPerDay := variables.FanOutMaxPostsPerAuthorPerDay
	if maxPerDay <= 0 || authorID <= 0 || redis.FanOutAuthorWindows == nil || !redis.FanOutAuthorWindows.IsReady() {
		return false
	}

	cutoffMs := domain.TimeToMillis(domain.NowTime().Add(-variables.ShortTTL))
	_ = redis.FanOutAuthorWindows.ZRemRangeByScore(ctx, authorID, "-inf", strconv.FormatInt(cutoffMs, 10))

	count, err := redis.FanOutAuthorWindows.ZCard(ctx, authorID)
	if err != nil || count < int64(maxPerDay) {
		return false
	}

	// Idempotence : autoriser un éventuel rejeu pour un post déjà comptabilisé dans la fenêtre
	if _, errScore := redis.FanOutAuthorWindows.ZScore(ctx, authorID, strconv.FormatInt(postID, 10)); errScore == nil {
		return false
	}
	return true
}

// recordAuthorFanOut comptabilise le post dans la fenêtre glissante 24h de Fan-Out de l'auteur.
func recordAuthorFanOut(ctx context.Context, authorID, postID int64) {
	if authorID <= 0 || redis.FanOutAuthorWindows == nil || !redis.FanOutAuthorWindows.IsReady() {
		return
	}
	nowMs := float64(domain.NowMillis())
	_ = redis.FanOutAuthorWindows.ZAdd(ctx, authorID, nowMs, strconv.FormatInt(postID, 10))
	_ = redis.FanOutAuthorWindows.Expire(ctx, authorID, variables.ShortTTL)
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

			// ── ÉTAPE 1.5 : COUPE-CIRCUIT ANTI-FLOOD PAR AUTEUR (24H GLISSANTES) ─
			if isAuthorFanOutThrottled(ctx, authorID, postID) {
				numan_log.Info(ctx).
					Int64("author_id", authorID).
					Int64("post_id", postID).
					Int("max_per_day", variables.FanOutMaxPostsPerAuthorPerDay).
					Msg("FanOut annulé : plafond quotidien par auteur atteint (protection anti-flood Mailbox).")
				continue
			}

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

			// ── ÉTAPE 3 : DISTRIBUTION DE MASSE VIA BATCH REDIS (CLUSTER-SAFE) ─
			score := float64(domain.NowMillis())
			member := formatMailboxMember(postID, authorID)
			if errPipe := redis.FeedsMailbox.ZAddAndTrimBatch(ctx, targetIDs, score, member, int64(variables.FanOutRetentionLimit)); errPipe != nil {
				numan_log.Error(ctx).Err(errPipe).Int64("post_id", postID).Msg("Échec de l'exécution du pipeline de distribution FanOut.")
				continue
			}
			recordAuthorFanOut(ctx, authorID, postID)
		}
	}
}
