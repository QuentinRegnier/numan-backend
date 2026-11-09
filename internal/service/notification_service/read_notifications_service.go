package notification_service

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/notification_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : ACQUITTEMENT (MARQUAGE COMME LU) DES NOTIFICATIONS
// ############################################################################

// MarkNotificationsAsRead met à jour le curseur de lecture de l'utilisateur (Watermark)
// en O(1) et synchronise l'état lu sur l'ensemble de ses appareils connectés.
func MarkNotificationsAsRead(ctx context.Context, userID int64, input notification_models.ReadNotificationsInput) (notification_models.ReadNotificationsOutput, error) {

	// ── ÉTAPE 1 : ENREGISTREMENT INSTANTANÉ DU CURSEUR (RAM L1) ─────────────

	errRedis := redis.NotificationCursors.SetPrimitive(ctx, userID, input.ReadUpToID)
	if errRedis != nil {
		numan_log.Error(ctx).Err(errRedis).Int64("user_id", userID).Msg("Impossible de sauvegarder le curseur de lecture des notifications")
		return notification_models.ReadNotificationsOutput{}, numan_error.NewInternal()
	}

	// ── ÉTAPE 2 : SYNCHRONISATION MULTI-APPAREILS (WEBSOCKETS) ──────────────

	websocketPayload := map[string]any{
		"read_up_to_id": input.ReadUpToID,
		"read_at":       domain.NowTime().Format(time.RFC3339),
	}

	errBroadcast := realtime_service.DistributeToUsers(ctx, variables.NotificationRead, websocketPayload, []int64{userID})
	if errBroadcast != nil {
		numan_log.Warn(ctx).Err(errBroadcast).Msg("Échec de la distribution WS pour " + variables.NotificationRead)
	}

	// ── ÉTAPE 3 : MARQUAGE D'ACTIVITÉ (DIRTY FLAG) ──────────────────────────

	latestActivityTimestampMs := cache_service.TouchActivityTimestamp(ctx, userID)

	return notification_models.ReadNotificationsOutput{
		ActivityUpdateAt: latestActivityTimestampMs,
	}, nil // Ne bloque pas le client HTTP pour une erreur WebSocket
}
