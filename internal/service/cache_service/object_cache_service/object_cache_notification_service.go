package object_cache_service

import (
	"context"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/notification_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/vmihailenco/msgpack/v5"
)

// ############################################################################
// # SERVICE : OBJECT CACHE (NOTIFICATIONS LFU & PIPELINE D'HYDRATATION)
// ############################################################################

// SetNotificationInObjectCache sauvegarde une notification dans le cache LFU.
func SetNotificationInObjectCache(ctx context.Context, notificationPayload notification_models.NotificationPayload) error {
	errRedis := redis.NotificationsObj.SetObject(ctx, notificationPayload.ID, notificationPayload)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de sauvegarder la notification dans l'Object Cache.",
			errRedis,
		)
	}
	return nil
}

// GetNotificationsView est le Pipeline d'Hydratation L1 -> L2 pour les notifications.
// Renvoie strictement les Payloads bruts (La couche haute s'occupera de forger la View).
func GetNotificationsView(ctx context.Context, targetNotificationIDs []int64) ([]notification_models.NotificationPayload, error) {
	if len(targetNotificationIDs) == 0 {
		return []notification_models.NotificationPayload{}, nil
	}

	finalHydratedNotificationsList := make([]notification_models.NotificationPayload, 0, len(targetNotificationIDs))
	temporaryNotificationsMap := make(map[int64]notification_models.NotificationPayload)

	// ── ÉTAPE 1 : TENTATIVE L1 (MGET) ───────────────────────────────────────
	mgetResult, errMGet := redis.NotificationsObj.GetMany(ctx, targetNotificationIDs)
	if errMGet != nil {
		numan_log.Warn(ctx).Err(errMGet).Msg("Erreur Redis MGET lors de la récupération des notifications (fallback L2 déclenché)")
		mgetResult = &redis.GetManyResult{MissingIDs: targetNotificationIDs}
	} else {
		for notificationID, binaryData := range mgetResult.Found {
			var notificationPayload notification_models.NotificationPayload
			if errDecode := msgpack.Unmarshal(binaryData, &notificationPayload); errDecode == nil {
				temporaryNotificationsMap[notificationID] = notificationPayload
			} else {
				numan_log.Warn(ctx).Err(errDecode).Entity("notification", notificationID).Msg("Échec du décodage MessagePack d'une notification en L1")
				mgetResult.MissingIDs = append(mgetResult.MissingIDs, notificationID)
			}
		}
	}

	// ── ÉTAPE 2 : FALLBACK L2 (MONGO WARM STORAGE) ──────────────────────────
	if len(mgetResult.MissingIDs) > 0 {
		mongoNotificationsList, errMongo := mongo.MongoLoadNotificationsByIDs(ctx, mgetResult.MissingIDs)
		if errMongo == nil {
			for _, mongoNotification := range mongoNotificationsList {
				temporaryNotificationsMap[mongoNotification.ID] = mongoNotification

				// PROMOTION L2 -> L1 (Auto-guérison asynchrone)
				go func(notifToPromote notification_models.NotificationPayload) {
					backgroundCtx := context.Background()
					if err := SetNotificationInObjectCache(backgroundCtx, notifToPromote); err != nil {
						numan_log.Warn(backgroundCtx).Err(err).Entity("notification", notifToPromote.ID).Action(numan_log.ActionHydrate).Msg("Échec de la promotion L2->L1 de la notification")
					}
				}(mongoNotification)
			}
		} else {
			numan_log.Warn(ctx).Err(errMongo).Msg("Échec L2 lors du chargement des notifications par IDs")
		}
	}

	// ── ÉTAPE 3 : ASSEMBLAGE FINAL ──────────────────────────────────────────
	for _, targetID := range targetNotificationIDs {
		if notificationPayload, isExists := temporaryNotificationsMap[targetID]; isExists {
			finalHydratedNotificationsList = append(finalHydratedNotificationsList, notificationPayload)
		}
	}

	return finalHydratedNotificationsList, nil
}
