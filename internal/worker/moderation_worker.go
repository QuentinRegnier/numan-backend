package worker

import (
	"context"
	"math"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ClockFunc permet d'injecter une horloge déterministe lors des tests unitaires (§B.4).
type ClockFunc func() time.Time

var defaultClock ClockFunc = time.Now

// SetModerationWorkerClock injecte une horloge personnalisée (utilisé pour les tests).
func SetModerationWorkerClock(c ClockFunc) func() {
	prev := defaultClock
	if c == nil {
		defaultClock = time.Now
	} else {
		defaultClock = c
	}
	return func() {
		defaultClock = prev
	}
}

func getWorkerTime() time.Time {
	if defaultClock != nil {
		return defaultClock()
	}
	return time.Now()
}

// StartModerationAgingCron lance la tâche périodique de réévaluation de M_temps sur la file active.
func StartModerationAgingCron(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du Cron de vieillissement de la file de modération (M_temps)...")
	go func() {
		interval := variables.ModerationAgingCronInterval
		if interval <= 0 {
			interval = 5 * time.Minute
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				numan_log.Info(ctx).Msg("Arrêt du Cron de vieillissement de la modération.")
				return
			case <-ticker.C:
				_, _ = ProcessModerationAgingBatch(ctx, getWorkerTime())
			}
		}
	}()
}

// StartModerationExpirationWorker surveille et applique les expirations des verrous InProgress (48h) et minuteurs WaitingForInfo (7j).
func StartModerationExpirationWorker(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du Worker d'expiration des verrous et minuteurs de modération...")
	go func() {
		interval := variables.ModerationExpirationCronTick
		if interval <= 0 {
			interval = 1 * time.Minute
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				numan_log.Info(ctx).Msg("Arrêt du Worker d'expiration de la modération.")
				return
			case <-ticker.C:
				now := getWorkerTime()
				_, _ = ProcessModerationLockExpirations(ctx, now)
				_, _ = ProcessModerationWaitingInfoExpirations(ctx, now)
			}
		}
	}()
}

// ProcessModerationAgingBatch recalcule atomiquement le score de vieillissement M_temps sur tous les tickets Pending
// de la file ZSET active sans jamais journaliser par ticket (§B.2). Préserve strictement les scores +Inf.
func ProcessModerationAgingBatch(ctx context.Context, now time.Time) (int, error) {
	records, err := cache_service.ListModerationQueueRecords(ctx, variables.ModerationQueueActive, 0, 500)
	if err != nil {
		return 0, err
	}
	recomputedCount := 0
	for _, rec := range records {
		if rec == nil || rec.ReportID <= 0 {
			continue
		}
		// Les tickets d'urgence vitale (+Inf) conservent leur priorité infinie immuable
		if rec.IsInfiniteUrgent || math.IsInf(rec.StaticBaseScore, 1) || math.IsInf(rec.PriorityScore, 1) {
			continue
		}

		ageHours := 0.0
		if rec.CreatedAt > 0 {
			createdTime := time.UnixMilli(rec.CreatedAt)
			if now.After(createdTime) {
				ageHours = now.Sub(createdTime).Hours()
			}
		}

		newScore := moderation_service.RecomputeAgedQueueScore(rec.StaticBaseScore, ageHours)
		rec.PriorityScore = newScore
		rec.UpdatedAt = now.UnixMilli()

		_ = cache_service.IndexTicketInModerationQueue(ctx, variables.ModerationQueueActive, rec.ReportID, newScore)
		_ = cache_service.SaveModerationTicket(ctx, rec)
		recomputedCount++
	}

	if recomputedCount > 0 {
		numan_log.Info(ctx).
			Int("recomputed_tickets_count", recomputedCount).
			Msg("Vieillissement de la file de modération : lot recalculé avec succès.")
	}
	return recomputedCount, nil
}

// ProcessModerationLockExpirations libère les tickets restés InProgress plus de 48h sans résolution et les replace en Pending.
func ProcessModerationLockExpirations(ctx context.Context, now time.Time) (int, error) {
	nowMs := now.UnixMilli()
	expiredIDs, err := cache_service.GetExpiredTicketIDs(ctx, variables.ModerationExpirationLocks, nowMs, 100)
	if err != nil {
		return 0, err
	}
	releasedCount := 0
	for _, reportID := range expiredIDs {
		ticket, errGet := cache_service.GetModerationTicket(ctx, reportID)
		if errGet != nil || ticket == nil {
			_ = cache_service.ReleaseTicketLock(ctx, reportID)
			continue
		}
		if ticket.State == variables.ReportStateInProgress {
			_ = cache_service.ReleaseTicketLock(ctx, reportID)
			ticket.State = variables.ReportStatePending
			ticket.ClaimedByModID = 0
			ticket.LockExpiresAtMs = 0
			ticket.UpdatedAt = nowMs
			_ = cache_service.SaveModerationTicket(ctx, ticket)
			_ = cache_service.IndexTicketInModerationQueue(ctx, variables.ModerationQueueActive, ticket.ReportID, ticket.PriorityScore)
			releasedCount++
		} else {
			// Le ticket n'est plus InProgress : nettoyer le verrou orphelin
			_ = cache_service.ReleaseTicketLock(ctx, reportID)
		}
	}

	if releasedCount > 0 {
		numan_log.Warn(ctx).
			Int("expired_locks_count", releasedCount).
			Msg("Verrous de modération expirés (48h dépassées) : tickets réintégrés en Pending.")
	}
	return releasedCount, nil
}

// ProcessModerationWaitingInfoExpirations débloque les tickets WaitingForInfo dont le délai de 7 jours est expiré
// et les restitue en InProgress au même modérateur pour décision finale.
func ProcessModerationWaitingInfoExpirations(ctx context.Context, now time.Time) (int, error) {
	nowMs := now.UnixMilli()
	expiredIDs, err := cache_service.GetExpiredTicketIDs(ctx, variables.ModerationExpirationWaitingInfo, nowMs, 100)
	if err != nil {
		return 0, err
	}
	resumedCount := 0
	for _, reportID := range expiredIDs {
		ticket, errGet := cache_service.GetModerationTicket(ctx, reportID)
		if errGet != nil || ticket == nil {
			_ = cache_service.ClearTicketWaitingInfoExpiration(ctx, reportID)
			continue
		}
		if ticket.State == variables.ReportStateWaitingForInfo {
			_ = cache_service.ClearTicketWaitingInfoExpiration(ctx, reportID)
			ticket.State = variables.ReportStateInProgress
			ticket.WaitingUntilMs = 0
			ticket.LockExpiresAtMs = nowMs + int64(variables.ModerationLockTTL.Milliseconds())
			ticket.UpdatedAt = nowMs

			// Réactivation du verrou exclusif pour le même modérateur
			if ticket.ClaimedByModID > 0 {
				_, _ = cache_service.AcquireTicketLock(ctx, ticket.ReportID, ticket.ClaimedByModID, ticket.LockExpiresAtMs, variables.ModerationLockTTL)
			}
			_ = cache_service.SaveModerationTicket(ctx, ticket)
			resumedCount++
		} else {
			_ = cache_service.ClearTicketWaitingInfoExpiration(ctx, reportID)
		}
	}

	if resumedCount > 0 {
		numan_log.Info(ctx).
			Int("expired_waiting_info_count", resumedCount).
			Msg("Tickets en attente d'information expirés (7 jours) : restitution en InProgress.")
	}
	return resumedCount, nil
}
