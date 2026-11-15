package notification_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/notification_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
)

// ############################################################################
// # SERVICE : RÉCUPÉRATION PAGINÉE DES NOTIFICATIONS (CASCADE)
// ############################################################################

// GetNotifications orchestre la cascade de lecture L1 -> L2 avec auto-guérison de
// la Boîte aux lettres de l'utilisateur. Note: Les notifications n'ont pas de stockage froid L3.
func GetNotifications(ctx context.Context, callerID int64, input notification_models.GetNotificationsInput) ([]notification_models.NotificationView, error) {
	if callerID <= 0 {
		return nil, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est invalide.", nil)
	}
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if input.Limit == 0 {
		input.Limit = 50
	}

	// ── ÉTAPE 0 : MODE FORCE (PURGE RAM) ────────────────────────────────────

	if input.Force {
		_ = cache_service.PurgeNotificationsZSET(ctx, callerID)
	}

	var orderedNotificationIDs []int64
	var errCacheIndex error

	// ── ÉTAPE 1 : TENTATIVE L1 (INDEX ZSET REDIS) ───────────────────────────

	if input.Offset < 100 && !input.Force {
		orderedNotificationIDs, errCacheIndex = cache_service.GetNotificationIDsFromZSET(ctx, callerID, input.Offset, input.Limit)
	}

	// ── ÉTAPE 2 : DÉPASSEMENT OU CACHE MISS -> FALLBACK L2 (MONGODB) ────────

	if errCacheIndex != nil || len(orderedNotificationIDs) == 0 {
		notificationsFromMongo, errMongo := mongo.MongoLoadNotificationsPaginated(ctx, callerID, input.Limit, input.Offset)
		if errMongo != nil {
			numan_log.Error(ctx).Err(errMongo).Int64("user_id", callerID).Msg("Erreur L2 lors de la récupération des notifications")
			return nil, numan_error.NewInternal()
		}

		// AUTO-GUÉRISON L2 -> L1 (Mise en cache ZSET et Object JSON en parallèle)
		go func(notifsBatch []notification_models.NotificationPayload, currentOffset int64) {
			backgroundCtx := context.Background()
			for _, notificationPayload := range notifsBatch {
				_ = object_cache_service.SetNotificationInObjectCache(backgroundCtx, notificationPayload)
				if currentOffset < 100 {
					_ = cache_service.AddNotificationToZSET(backgroundCtx, notificationPayload.UserID, notificationPayload.ID, notificationPayload.CreatedAt)
				}
			}
		}(notificationsFromMongo, input.Offset)

		// Hydratation immédiate depuis les payloads BDD sans repasser par le cache L1
		var mongoGeneratedViews []notification_models.NotificationView
		for _, notificationPayload := range notificationsFromMongo {
			mongoGeneratedViews = append(mongoGeneratedViews, hydrateNotificationView(ctx, callerID, notificationPayload))
		}
		return mongoGeneratedViews, nil
	}

	// ── ÉTAPE 3 : CACHE HIT -> HYDRATATION MASSIVE DEPUIS L1 (MGET) ─────────

	notificationsPayloadsFromCache, errObjectCache := object_cache_service.GetNotificationsView(ctx, orderedNotificationIDs)
	if errObjectCache != nil {
		numan_log.Error(ctx).Err(errObjectCache).Msg("Erreur L1 lors de la récupération MGET des objets de notifications")
		return nil, numan_error.NewInternal()
	}

	// ── ÉTAPE 4 : ASSEMBLAGE FINAL DES DTO (BOUCLE O(N)) ────────────────────

	finalHydratedViews := make([]notification_models.NotificationView, 0, len(notificationsPayloadsFromCache))
	for _, notificationPayload := range notificationsPayloadsFromCache {
		finalHydratedViews = append(finalHydratedViews, hydrateNotificationView(ctx, callerID, notificationPayload))
	}

	return finalHydratedViews, nil
}
