package sync_service

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/notification_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/sync_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service/notification_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : DELTA SYNC DU CENTRE D'ACTIVITÉS
// ############################################################################

// SyncActivity vérifie le delta temporel et les curseurs ID pour ne renvoyer
// que les nouvelles notifications sans hydratations inutiles.
func SyncActivity(ctx context.Context, callerID int64, input sync_models.SyncActivityInput) (sync_models.SyncActivityOutput, error) {

	syncOutput := sync_models.SyncActivityOutput{
		NeedUpdate: false,
	}

	// ── ÉTAPE 1 : FETCH DES DERNIERS ÉVÉNEMENTS (L1 -> L2) ──────────────────

	// L'offset 0 et limit 100 garantissent l'aspiration du sommet du ZSET chronologique.
	requestPayload := notification_models.GetNotificationsInput{
		Limit:  int64(variables.MaxZsetNotification),
		Offset: 0,
		Force:  false,
	}

	recentNotificationViews, errFetch := notification_service.GetNotifications(ctx, callerID, requestPayload)
	if errFetch != nil {
		numan_log.Error(ctx).Err(errFetch).Int64("user_id", callerID).Msg("Erreur critique lors du Fetch L1/L2 pour le SyncActivity")
		return syncOutput, numan_error.NewInternal()
	}

	if len(recentNotificationViews) == 0 {
		syncOutput.ServerUpdated = time.Now().UnixMilli()
		return syncOutput, nil
	}

	// ── ÉTAPE 2 : ÉVALUATION DU DELTA TEMPOREL (RÉSOLUTION CONFLIT) ─────────

	latestNotificationView := recentNotificationViews[0]
	latestServerTimestamp := latestNotificationView.CreatedAt
	syncOutput.ServerUpdated = latestServerTimestamp

	// Si l'événement le plus récent côté serveur est <= au cache client,
	// ET que le dernier Snowflake ID lu est couvert, le client est parfaitement à jour.
	if latestServerTimestamp <= input.ClientUpdatedAt && latestNotificationView.ID <= input.ReadUpToID {
		return syncOutput, nil
	}

	// ── ÉTAPE 3 : FILTRAGE CHIRURGICAL EN RAM (O(N)) ────────────────────────

	var unreadNotificationViews []notification_models.NotificationView

	for _, notificationView := range recentNotificationViews {
		if notificationView.ID > input.ReadUpToID || notificationView.CreatedAt > input.ClientUpdatedAt {
			unreadNotificationViews = append(unreadNotificationViews, notificationView)
		} else {
			// Le ZSET est trié de manière strictement décroissante chronologiquement.
			// La première notification déjà connue par le client valide la fin du delta.
			break
		}
	}

	if len(unreadNotificationViews) == 0 {
		return syncOutput, nil
	}

	syncOutput.NeedUpdate = true
	syncOutput.Notifications = unreadNotificationViews

	return syncOutput, nil
}
