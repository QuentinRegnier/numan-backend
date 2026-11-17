package worker

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	pgRepo "github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/user_recommendation_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # WORKER & CRON : SYSTÈME DE RECOMMANDATION D'UTILISATEURS
// ############################################################################

// UserRecoCronStats regroupe les métriques d'exécution d'un cycle du Cron de rafraîchissement.
type UserRecoCronStats struct {
	UsersScanned    int
	JobsEnqueued    int
	SkippedLocked   int
	SkippedInactive int
	ErrorsCount     int
	DurationMs      int64
}

// startUserRecommendationCron lance la tâche planifiée (toutes les 24–48 heures) qui rafraîchit
// par lots les listes de recommandation d'utilisateurs actifs sans saturer Redis.
func startUserRecommendationCron(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du Cron de rafraîchissement des recommandations d'utilisateurs...")

	go func() {
		interval := variables.UserRecoCronInterval
		if interval < 24*time.Hour {
			interval = 24 * time.Hour
		}
		if interval > 48*time.Hour {
			interval = 48 * time.Hour
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				numan_log.Info(ctx).Msg("Arrêt gracieux du Cron de recommandation d'utilisateurs.")
				return
			case <-ticker.C:
				_ = RunUserRecommendationRefreshCronCycle(ctx)
			}
		}
	}()
}

// handleUserRecommendationEvents traite les événements ActionGenerateUserRecommendations dépilés par les workers Redis.
func handleUserRecommendationEvents(ctx context.Context, events []redis.AsyncEvent) {
	if len(events) == 0 {
		return
	}

	processedUsers := make(map[int64]struct{})

	for _, evt := range events {
		if evt.Action != redis.ActionGenerateUserRecommendations && evt.Type != redis.EntityUserRecommendation {
			continue
		}
		if evt.Action != redis.ActionGenerateUserRecommendations {
			continue
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		targetUserID := evt.ID
		if evt.Payload != nil {
			if rawBytes, errMarshal := json.Marshal(evt.Payload); errMarshal == nil {
				var jobPayload user_recommendation_models.GenerateUserRecommendationsJobPayload
				if errUnmarshal := json.Unmarshal(rawBytes, &jobPayload); errUnmarshal == nil && jobPayload.UserID > 0 {
					targetUserID = jobPayload.UserID
				}
			}
		}

		if targetUserID <= 0 {
			continue
		}
		if _, alreadyDone := processedUsers[targetUserID]; alreadyDone {
			continue
		}
		processedUsers[targetUserID] = struct{}{}

		if err := user_recommendation_service.GenerateUserRecommendations(ctx, targetUserID); err != nil {
			numan_log.Error(ctx).
				Err(err).
				Int64("user_id", targetUserID).
				Msg("Worker UserRecommendation : Échec de la génération des recommandations pour l'utilisateur")
		}
	}
}

// RunUserRecommendationRefreshCronCycle exécute un cycle complet de rafraîchissement par lots (batch)
// pour les utilisateurs actifs et éligibles en respectant les verrous de calcul et l'arrêt gracieux.
func RunUserRecommendationRefreshCronCycle(ctx context.Context) UserRecoCronStats {
	startTime := time.Now()
	stats := UserRecoCronStats{}

	batchSize := variables.UserRecoCronBatchSize
	if batchSize <= 0 {
		batchSize = 200
	}

	activeWindowDays := variables.UserRecoActiveWindowDays
	if activeWindowDays <= 0 {
		activeWindowDays = 30
	}
	cutoffTimeMs := domain.TimeToMillis(domain.NowTime().Add(-time.Duration(activeWindowDays) * 24 * time.Hour))

	offset := 0
	for {
		select {
		case <-ctx.Done():
			stats.DurationMs = time.Since(startTime).Milliseconds()
			numan_log.Info(ctx).
				Int("users_scanned", stats.UsersScanned).
				Int("jobs_enqueued", stats.JobsEnqueued).
				Int("skipped_locked", stats.SkippedLocked).
				Int("skipped_inactive", stats.SkippedInactive).
				Int("errors_count", stats.ErrorsCount).
				Int64("duration_ms", stats.DurationMs).
				Msg("Cron UserRecommendation interrompu proprement par arrêt du contexte.")
			return stats
		default:
		}

		batchUserIDs, errBatch := loadUserBatchForRecoCron(ctx, batchSize, offset)
		if errBatch != nil {
			stats.ErrorsCount++
			numan_log.Error(ctx).Err(errBatch).Int("offset", offset).Msg("Cron UserRecommendation : Erreur lors de la lecture d'un lot d'utilisateurs")
			break
		}
		if len(batchUserIDs) == 0 {
			break
		}

		for _, userID := range batchUserIDs {
			select {
			case <-ctx.Done():
				break
			default:
			}

			if userID <= 0 {
				continue
			}
			stats.UsersScanned++

			if !isUserActiveAndEligibleForRecoCron(ctx, userID, cutoffTimeMs) {
				stats.SkippedInactive++
				continue
			}

			// Respect du verrou de calcul (lock:user_reco:compute:{user_id})
			acquired, errLock := cache_service.AcquireUserRecoComputeLock(ctx, userID)
			if errLock != nil {
				stats.ErrorsCount++
				continue
			}
			if !acquired {
				stats.SkippedLocked++
				continue
			}

			if errEnq := user_recommendation_service.EnqueueGenerateUserRecommendations(ctx, userID, false, "cron_refresh"); errEnq != nil {
				stats.ErrorsCount++
				numan_log.Warn(ctx).Err(errEnq).Int64("user_id", userID).Msg("Cron UserRecommendation : Échec de l'envoi du job dans la file Redis")
				continue
			}
			stats.JobsEnqueued++
		}

		offset += len(batchUserIDs)
		if len(batchUserIDs) < batchSize {
			break
		}
	}

	stats.DurationMs = time.Since(startTime).Milliseconds()
	numan_log.Info(ctx).
		Int("users_scanned", stats.UsersScanned).
		Int("jobs_enqueued", stats.JobsEnqueued).
		Int("skipped_locked", stats.SkippedLocked).
		Int("skipped_inactive", stats.SkippedInactive).
		Int("errors_count", stats.ErrorsCount).
		Int64("duration_ms", stats.DurationMs).
		Msg("Cron UserRecommendation : Cycle de rafraîchissement terminé avec succès.")

	return stats
}

// loadUserBatchForRecoCron récupère un lot paginé d'identifiants utilisateurs depuis PostgreSQL L3
// ou depuis l'index lexicographique Redis L1 si PostgreSQL n'est pas initialisé.
func loadUserBatchForRecoCron(ctx context.Context, limit int, offset int) ([]int64, error) {
	if postgres.PostgresDB != nil {
		usersBatch, errPg := pgRepo.FuncLoadUsersPaginated(ctx, limit, offset)
		if errPg != nil {
			return nil, errPg
		}
		ids := make([]int64, 0, len(usersBatch))
		for _, u := range usersBatch {
			if u.ID > 0 {
				ids = append(ids, u.ID)
			}
		}
		return ids, nil
	}

	if redis.UsersLex != nil {
		start := int64(offset)
		stop := int64(offset + limit - 1)
		lexEntries, errLex := redis.UsersLex.ZRange(ctx, variables.LexicographicGlobalKey, start, stop)
		if errLex != nil {
			return nil, errLex
		}
		ids := make([]int64, 0, len(lexEntries))
		for _, entry := range lexEntries {
			parts := strings.Split(entry, ":")
			if len(parts) == 2 {
				if id := pkg.ParseInt64(parts[1]); id > 0 {
					ids = append(ids, id)
				}
			}
		}
		return ids, nil
	}

	return []int64{}, nil
}

// isUserActiveAndEligibleForRecoCron vérifie qu'un utilisateur est éligible (non banni, non désactivé)
// et a eu une activité récente (>= cutoffTimeMs ou en ligne).
func isUserActiveAndEligibleForRecoCron(ctx context.Context, userID int64, cutoffTimeMs int64) bool {
	if redis.Users != nil {
		var u auth_models.UserPayload
		if err := redis.Users.GetObject(ctx, userID, &u); err == nil && u.ID != 0 {
			if u.Banned || u.Desactivated {
				return false
			}
			if u.UpdatedAt >= cutoffTimeMs || u.CreatedAt >= cutoffTimeMs {
				return true
			}
		}
	}

	if cache_service.IsUserOnline(ctx, userID) {
		return true
	}

	if profile, errTel := cache_service.GetTelemetryProfile(ctx, userID); errTel == nil && profile.TimestampMs >= cutoffTimeMs {
		return true
	}

	if redis.InboxActivity != nil {
		if ts, errAct := redis.InboxActivity.GetInt64(ctx, userID); errAct == nil && ts >= cutoffTimeMs {
			return true
		}
	}

	return false
}
