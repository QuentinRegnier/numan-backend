package cache_service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	goredis "github.com/go-redis/redis/v8"
)

func ticketLockKey(reportID int64) string {
	return fmt.Sprintf("lock:%d", reportID)
}

// SaveModerationTicket persiste l'état complet du ticket de modération dans redis.ModerationTickets.
func SaveModerationTicket(ctx context.Context, ticket *moderation_models.ModerationTicketRecord) error {
	if ticket == nil || ticket.ReportID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "identifiant de ticket invalide", nil)
	}
	if redis.ModerationTickets == nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Cache de modération non initialisé", errors.New("ModerationTickets nil"))
	}
	if err := redis.ModerationTickets.SetObject(ctx, ticket.ReportID, ticket); err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible d'enregistrer le ticket de modération en cache.", err)
	}
	return nil
}

// GetModerationTicket récupère l'enregistrement interne d'un ticket depuis redis.ModerationTickets.
func GetModerationTicket(ctx context.Context, reportID int64) (*moderation_models.ModerationTicketRecord, error) {
	if redis.ModerationTickets == nil {
		return nil, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Cache de modération non initialisé", errors.New("ModerationTickets nil"))
	}
	var rec moderation_models.ModerationTicketRecord
	if err := redis.ModerationTickets.GetObject(ctx, reportID, &rec); err != nil {
		if errors.Is(err, goredis.Nil) {
			return nil, numan_error.NewNotFound(numan_error.CodeNotFound, "Ticket de modération introuvable", err)
		}
		return nil, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de lire le ticket de modération depuis le cache.", err)
	}
	return &rec, nil
}

// DeleteModerationTicket supprime l'objet ticket du cache L1 (utilisé après archivage froid).
func DeleteModerationTicket(ctx context.Context, reportID int64) error {
	if redis.ModerationTickets == nil {
		return nil
	}
	_ = RemoveTicketFromAllModerationQueues(ctx, reportID)
	_ = ReleaseTicketLock(ctx, reportID)
	_ = ClearTicketWaitingInfoExpiration(ctx, reportID)
	return redis.ModerationTickets.DeleteObject(ctx, reportID)
}

// IndexTicketInModerationQueue insère ou met à jour le score d'un ticket dans l'une des files ZSET ("active", "peer_review", "final_vote", "admin").
// Supporte nativement les scores +Inf (math.Inf(1)) pour les catégories vitales 6 et 9.
func IndexTicketInModerationQueue(ctx context.Context, queueName string, reportID int64, score float64) error {
	if redis.ModerationQueues == nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Files ZSET de modération non initialisées", errors.New("ModerationQueues nil"))
	}
	member := strconv.FormatInt(reportID, 10)
	if err := redis.ModerationQueues.ZAdd(ctx, queueName, score, member); err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible d'indexer le ticket dans la file de modération.", err)
	}
	return nil
}

// RemoveTicketFromModerationQueue retire un ticket d'une sous-file ZSET spécifique.
func RemoveTicketFromModerationQueue(ctx context.Context, queueName string, reportID int64) error {
	if redis.ModerationQueues == nil {
		return nil
	}
	member := strconv.FormatInt(reportID, 10)
	return redis.ModerationQueues.ZRem(ctx, queueName, member)
}

// RemoveTicketFromAllModerationQueues retire un ticket de toutes les sous-files ZSET de modération.
func RemoveTicketFromAllModerationQueues(ctx context.Context, reportID int64) error {
	if redis.ModerationQueues == nil {
		return nil
	}
	member := strconv.FormatInt(reportID, 10)
	queues := []string{
		variables.ModerationQueueActive,
		variables.ModerationQueuePeerReview,
		variables.ModerationQueueFinalVote,
		variables.ModerationQueueAdmin,
	}
	for _, q := range queues {
		_ = redis.ModerationQueues.ZRem(ctx, q, member)
	}
	return nil
}

// ListModerationQueueTicketIDs retourne les identifiants de tickets et leurs scores par ordre décroissant (ZREVRANGE WITHSCORES).
func ListModerationQueueTicketIDs(ctx context.Context, queueName string, offset, limit int64) ([]int64, []float64, error) {
	if redis.ModerationQueues == nil {
		return nil, nil, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Files ZSET de modération non initialisées", errors.New("ModerationQueues nil"))
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	stop := offset + limit - 1
	items, err := redis.ModerationQueues.ZRevRangeWithScores(ctx, queueName, offset, stop)
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return nil, nil, nil
		}
		return nil, nil, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de lister la file de modération.", err)
	}
	ids := make([]int64, 0, len(items))
	scores := make([]float64, 0, len(items))
	for _, z := range items {
		memberStr := fmt.Sprintf("%v", z.Member)
		id, errParse := strconv.ParseInt(memberStr, 10, 64)
		if errParse != nil || id <= 0 {
			continue
		}
		ids = append(ids, id)
		scores = append(scores, z.Score)
	}
	return ids, scores, nil
}

// ListModerationQueueRecords hydrate les enregistrements complets d'une sous-file ZSET par priorité décroissante.
func ListModerationQueueRecords(ctx context.Context, queueName string, offset, limit int64) ([]*moderation_models.ModerationTicketRecord, error) {
	ids, scores, err := ListModerationQueueTicketIDs(ctx, queueName, offset, limit)
	if err != nil {
		return nil, err
	}
	records := make([]*moderation_models.ModerationTicketRecord, 0, len(ids))
	for i, id := range ids {
		rec, errGet := GetModerationTicket(ctx, id)
		if errGet != nil || rec == nil {
			continue
		}
		rec.PriorityScore = scores[i]
		records = append(records, rec)
	}
	return records, nil
}

// AcquireTicketLock pose un verrou atomique (SetNX + TTL 48h) et enregistre l'échéance dans le ZSET d'expiration.
func AcquireTicketLock(ctx context.Context, reportID int64, modID int64, expiresAtMs int64, ttl time.Duration) (bool, error) {
	if redis.ModerationTickets == nil || redis.ModerationExpirations == nil {
		return false, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Cache de modération non initialisé", errors.New("ModerationTickets nil"))
	}
	ok, err := redis.ModerationTickets.SetNX(ctx, ticketLockKey(reportID), strconv.FormatInt(modID, 10), ttl)
	if err != nil {
		return false, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible d'acquérir le verrou du ticket.", err)
	}
	if !ok {
		return false, nil
	}
	member := strconv.FormatInt(reportID, 10)
	_ = redis.ModerationExpirations.ZAdd(ctx, variables.ModerationExpirationLocks, float64(expiresAtMs), member)
	return true, nil
}

// ReleaseTicketLock libère le verrou exclusif d'un ticket et retire son échéance du ZSET d'expiration.
func ReleaseTicketLock(ctx context.Context, reportID int64) error {
	if redis.ModerationTickets != nil {
		_ = redis.ModerationTickets.DeletePrimitive(ctx, ticketLockKey(reportID))
	}
	if redis.ModerationExpirations != nil {
		_ = redis.ModerationExpirations.ZRem(ctx, variables.ModerationExpirationLocks, strconv.FormatInt(reportID, 10))
	}
	return nil
}

// ScheduleTicketWaitingInfoExpiration planifie le minuteur de 7 jours pour un ticket en état WaitingForInfo.
func ScheduleTicketWaitingInfoExpiration(ctx context.Context, reportID int64, expiresAtMs int64) error {
	if redis.ModerationExpirations == nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "ZSET d'expiration non initialisé", errors.New("ModerationExpirations nil"))
	}
	member := strconv.FormatInt(reportID, 10)
	if err := redis.ModerationExpirations.ZAdd(ctx, variables.ModerationExpirationWaitingInfo, float64(expiresAtMs), member); err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de planifier l'expiration WaitingForInfo.", err)
	}
	return nil
}

// ClearTicketWaitingInfoExpiration supprime le minuteur WaitingForInfo d'un ticket.
func ClearTicketWaitingInfoExpiration(ctx context.Context, reportID int64) error {
	if redis.ModerationExpirations == nil {
		return nil
	}
	return redis.ModerationExpirations.ZRem(ctx, variables.ModerationExpirationWaitingInfo, strconv.FormatInt(reportID, 10))
}

// GetExpiredTicketIDs retourne la liste des tickets dont l'échéance (lock 48h ou waiting_info 7j) est dépassée à l'instant nowMs.
func GetExpiredTicketIDs(ctx context.Context, expirationSet string, nowMs int64, limit int64) ([]int64, error) {
	if redis.ModerationExpirations == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	members, err := redis.ModerationExpirations.ZRangeByScoreWithLimit(ctx, expirationSet, nowMs, limit)
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return nil, nil
		}
		return nil, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de lire les échéances expirées.", err)
	}
	ids := make([]int64, 0, len(members))
	for _, m := range members {
		if id, errParse := strconv.ParseInt(m, 10, 64); errParse == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// GetOrInitModeratorStats récupère ou initialise le profil de gouvernance (T_mod, BlameCount, BlameHistory) d'un utilisateur/modérateur.
func GetOrInitModeratorStats(ctx context.Context, userID int64, defaultGrade int) (*moderation_models.ModeratorStatsRecord, error) {
	if redis.ModeratorStats == nil {
		return nil, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Cache ModeratorStats non initialisé", errors.New("ModeratorStats nil"))
	}
	var stats moderation_models.ModeratorStatsRecord
	err := redis.ModeratorStats.GetObject(ctx, userID, &stats)
	if err == nil && stats.UserID == userID {
		return &stats, nil
	}
	if err != nil && !errors.Is(err, goredis.Nil) {
		return nil, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de lire les statistiques de gouvernance.", err)
	}

	grade := defaultGrade
	modTrust := variables.ModTrustInitial
	blameCount := 0
	if userLite, errLite := GetUserLite(ctx, userID); errLite == nil && userLite.ID > 0 {
		if userLite.Grade > 0 {
			grade = userLite.Grade
		}
		if userLite.ModeratorTrustScore > 0 {
			modTrust = userLite.ModeratorTrustScore
		}
		if userLite.BlameCount > 0 {
			blameCount = userLite.BlameCount
		}
	}

	stats = moderation_models.ModeratorStatsRecord{
		UserID:              userID,
		ModeratorTrustScore: modTrust,
		Grade:               grade,
		BlameCount:          blameCount,
		UpdatedAt:           domain.NowMillis(),
	}
	_ = SaveModeratorStats(ctx, &stats)
	return &stats, nil
}

// SaveModeratorStats persiste le profil de gouvernance (T_mod, BlameCount, BlameHistory) dans redis.ModeratorStats.
func SaveModeratorStats(ctx context.Context, stats *moderation_models.ModeratorStatsRecord) error {
	if stats == nil || stats.UserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "statistiques modérateur invalides", nil)
	}
	if redis.ModeratorStats == nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Cache ModeratorStats non initialisé", errors.New("ModeratorStats nil"))
	}
	stats.UpdatedAt = domain.NowMillis()
	if err := redis.ModeratorStats.SetObject(ctx, stats.UserID, stats); err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de sauvegarder les statistiques de gouvernance.", err)
	}
	return nil
}
