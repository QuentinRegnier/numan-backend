package notification_service

import (
	"context"
	"encoding/json"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/notification_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// Structure locale privée pour casser la dépendance cyclique vers le package worker
type pushJob struct {
	UserID    int64  `json:"user_id"`
	EventType string `json:"event_type"`
	Payload   any    `json:"payload"`
}

// ############################################################################
// # SERVICE : DISTRIBUTION GLOBALE DES NOTIFICATIONS (WS & PUSH FCM)
// ############################################################################

// DispatchNotification forge la notification, la sauvegarde (L1+L2), la diffuse
// en temps réel (WS), et l'envoie au Push Worker FCM si les paramètres l'autorisent.
func DispatchNotification(ctx context.Context, targetUserID int64, actorUserID int64, eventType string, targetResourceID int64) error {

	// ── ÉTAPE 1 : RÈGLE MÉTIER (AUTO-NOTIFICATION INTERDITE) ────────────────
	if targetUserID == actorUserID {
		return nil // Succès silencieux, on ne spam pas l'utilisateur avec ses propres actions.
	}

	// ── ÉTAPE 2 : PRÉPARATION DU PAYLOAD ────────────────────────────────────
	notificationPayload := notification_models.NotificationPayload{
		ID:        pkg.GenerateID(),
		UserID:    targetUserID,
		ActorID:   actorUserID,
		Type:      eventType,
		TargetID:  targetResourceID,
		IsRead:    false,
		CreatedAt: domain.NowMillis(),
	}

	if err := notificationPayload.Validate(); err != nil {
		return err
	}

	// ── ÉTAPE 3 : CACHE L1 IMMÉDIAT (JSON + ZSET) ───────────────────────────
	_ = object_cache_service.SetNotificationInObjectCache(ctx, notificationPayload)
	_ = cache_service.AddNotificationToZSET(ctx, targetUserID, notificationPayload.ID, notificationPayload.CreatedAt)

	// ── ÉTAPE 4 : PERSISTANCE L2 ASYNCHRONE (MONGODB) ───────────────────────
	errQueue := redis.EnqueueDB(ctx, notificationPayload.ID, targetUserID, redis.EntityNotification, redis.ActionCreate, notificationPayload, redis.TargetMongo)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("user_id", targetUserID).Msg("Échec de la persistance L2 d'une notification")
		return numan_error.NewInternal()
	}

	// ── ÉTAPE 5 : TEMPS RÉEL (WEBSOCKETS) ───────────────────────────────────
	notificationView := hydrateNotificationView(ctx, targetUserID, notificationPayload)
	eventChannelName := "notification." + eventType
	_ = realtime_service.DistributeToUsers(ctx, eventChannelName, notificationView, []int64{targetUserID})

	// ── ÉTAPE 6 : MATRICE DE ROUTAGE DES NOTIFICATIONS PUSH ─────────────────

	// Filtre de présence : Si l'utilisateur est actif, on stoppe le Push (le WS a suffi)
	if cache_service.IsUserOnline(ctx, targetUserID) {
		return nil
	}

	userSettingsPayload, errSettings := object_cache_service.GetUserSettingsCascade(ctx, targetUserID)
	if errSettings != nil {
		numan_log.Error(ctx).Err(errSettings).Int64("user_id", targetUserID).Msg("Échec de de la lecture des droits de l'utilisateur")
		return numan_error.NewInternal() // Si on ne peut pas lire les droits, on refuse l'envoi
	}

	if !userSettingsPayload.Notifications.MasterPushEnabled {
		return nil // Mode silencieux complet activé
	}

	isPushAuthorized := false

	// Aiguillage granulaire selon les préférences de l'utilisateur
	switch eventType {
	case variables.EventPostLiked, variables.EventCommentLiked:
		isPushAuthorized = userSettingsPayload.Notifications.NotifyLikes
	case variables.EventCommentAdded:
		isPushAuthorized = userSettingsPayload.Notifications.NotifyComments
	case variables.EventRelationFollowed:
		isPushAuthorized = userSettingsPayload.Notifications.NotifyNewFollower
	case variables.EventFriendshipEst:
		isPushAuthorized = userSettingsPayload.Notifications.NotifyFriendRequest
	case variables.EventGroupInvited:
		isPushAuthorized = userSettingsPayload.Notifications.NotifyGroupInvites
	default:
		isPushAuthorized = true // Fallback autorisé pour les alertes systèmes vitales
	}

	// ── ÉTAPE 7 : EXPÉDITION AU WORKER FCM ──────────────────────────────────
	if isPushAuthorized {
		firebasePushJob := pushJob{
			UserID:    targetUserID,
			EventType: eventType,
			Payload: map[string]interface{}{
				"actor_id":  actorUserID,
				"target_id": targetResourceID,
			},
		}

		if jobBytes, errMarshal := json.Marshal(firebasePushJob); errMarshal == nil {
			_ = redis.WorkerQueue.LPush(ctx, variables.WorkerQueueFirebase, jobBytes)
		} else {
			numan_log.Error(ctx).Err(errMarshal).Msg("Impossible de sérialiser le Push Job FCM")
		}
	}

	return nil
}
