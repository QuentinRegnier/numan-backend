package cache_service

import (
	"context"
	"strconv"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// ############################################################################
// # SERVICE : CACHE DES NOTIFICATIONS (ZSET)
// ############################################################################

// AddNotificationToZSET plafonne le ZSET à 100 éléments via script Lua et rafraîchit le TTL.
func AddNotificationToZSET(ctx context.Context, userID int64, notificationID int64, timestampMs int64) error {
	notificationIDStr := strconv.FormatInt(notificationID, 10)
	errRedis := redis.NotificationsZSet.ZAddWithCap(ctx, userID, float64(timestampMs), notificationIDStr, 100)

	if errRedis != nil {
		numan_log.Error(ctx).Err(errRedis).Int64("user_id", userID).Msg("Impossible d'ajouter la notification au ZSET")
		return errRedis
	}

	err := redis.NotificationsZSet.RefreshTTL(ctx, userID)
	if err != nil {
		numan_log.Warn(ctx).Err(err).Int64("user_id", userID).Msg("Impossible de prolonger le TTL du ZSET des notifications")
		return err
	}
	return nil
}

// GetNotificationIDsFromZSET récupère un batch d'IDs paginés depuis le ZSET de l'utilisateur.
func GetNotificationIDsFromZSET(ctx context.Context, userID int64, offset int64, limit int64) ([]int64, error) {
	idStringsList, errRedis := redis.NotificationsZSet.ZRevRange(ctx, userID, offset, offset+limit-1)
	if errRedis != nil {
		numan_log.Error(ctx).Err(errRedis).Int64("user_id", userID).Msg("Erreur de récupération des IDs de notification dans le ZSET")
		return nil, numan_error.NewInternal()
	}

	parsedNotificationIDs := pkg.ParseInt64List(idStringsList)

	// Prolongation de la vie du cache puisqu'il vient d'être accédé
	if len(parsedNotificationIDs) > 0 {
		err := redis.NotificationsZSet.RefreshTTL(ctx, userID)
		if err != nil {
			numan_log.Warn(ctx).Err(err).Int64("user_id", userID).Msg("Impossible de prolonger le TTL du ZSET des notifications")
			return nil, err
		}
	}

	return parsedNotificationIDs, nil
}

// PurgeNotificationsZSET détruit l'intégralité du cache des notifications de l'utilisateur.
func PurgeNotificationsZSET(ctx context.Context, userID int64) error {
	errRedis := redis.NotificationsZSet.DeleteObject(ctx, userID)
	if errRedis != nil {
		numan_log.Error(ctx).Err(errRedis).Msg("Erreur lors de la purge du ZSET des notifications")
		return numan_error.NewInternal()
	}
	return nil
}

// TouchActivityTimestamp signale une modification d'état (ex: marquage comme lu)
// et retourne le timestamp exact généré par le serveur.
func TouchActivityTimestamp(ctx context.Context, userID int64) int64 {
	currentTimestampMs := time.Now().UnixMilli()

	errRedis := redis.NotificationActivity.SetPrimitive(ctx, userID, currentTimestampMs)
	if errRedis != nil {
		numan_log.Warn(ctx).Err(errRedis).Int64("user_id", userID).Msg("Échec de la mise à jour du Timestamp d'Activité (Notification)")
	}

	return currentTimestampMs
}
