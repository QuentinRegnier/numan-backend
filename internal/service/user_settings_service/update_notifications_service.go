package user_settings_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : MISE À JOUR DES PARAMÈTRES DE NOTIFICATION
// ############################################################################

// UpdateNotifications fusionne les nouveaux réglages de notifications et délègue la sauvegarde au Write-Behind.
func UpdateNotifications(ctx context.Context, userID int64, input user_settings_models.UpdateNotificationsInput) (user_settings_models.UpdateNotificationsOutput, error) {
	if userID <= 0 {
		return user_settings_models.UpdateNotificationsOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est invalide.", nil)
	}
	if err := input.Validate(); err != nil {
		return user_settings_models.UpdateNotificationsOutput{}, err
	}

	// ── ÉTAPE 1 : RÉCUPÉRATION DES PARAMÈTRES (CASCADE L1 -> L2 -> L3) ──────
	userSettingsPayload, errCache := object_cache_service.GetUserSettingsCascade(ctx, userID)
	if errCache != nil || userSettingsPayload.ID == 0 {
		return user_settings_models.UpdateNotificationsOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Paramètres de l'utilisateur introuvables.", errCache)
	}

	// ── ÉTAPE 2 : REMPLACEMENT INTÉGRAL DES PARAMÈTRES ──────────────────────
	userSettingsPayload.Notifications.MasterPushEnabled = input.MasterPushEnabled
	userSettingsPayload.Notifications.MasterEmailEnabled = input.MasterEmailEnabled
	userSettingsPayload.Notifications.NotifyLikes = input.NotifyLikes
	userSettingsPayload.Notifications.NotifyComments = input.NotifyComments
	userSettingsPayload.Notifications.NotifyMentions = input.NotifyMentions
	userSettingsPayload.Notifications.NotifyNewFollower = input.NotifyNewFollower
	userSettingsPayload.Notifications.NotifyFriendRequest = input.NotifyFriendRequest
	userSettingsPayload.Notifications.NotifyMessages = input.NotifyMessages
	userSettingsPayload.Notifications.NotifyGroupInvites = input.NotifyGroupInvites

	userSettingsPayload.UpdatedAt = domain.NowMillis()

	if err := userSettingsPayload.Validate(); err != nil {
		return user_settings_models.UpdateNotificationsOutput{}, err
	}

	// ── ÉTAPE 3 : MISE À JOUR IMMÉDIATE L1 EN RAM ───────────────────────────
	if errSet := object_cache_service.SetUserSettings(ctx, userSettingsPayload); errSet != nil {
		numan_log.Warn(ctx).Err(errSet).Int64("user_id", userID).Msg("Impossible de mettre à jour les paramètres de notifications dans le cache L1")
	}

	// ── ÉTAPE 4 : NOTIFICATION TEMPS RÉEL (WEBSOCKET) ───────────────────────
	errBroadcast := realtime_service.DistributeToUsers(ctx, variables.NotificationSettingsUpdated, userSettingsPayload, []int64{userID})
	if errBroadcast != nil {
		numan_log.Warn(ctx).Err(errBroadcast).Msg("Échec de la distribution WebSocket pour la mise à jour des notifications")
	}

	// ── ÉTAPE 5 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────────
	errQueue := redis.EnqueueDB(ctx, userSettingsPayload.ID, userID, redis.EntityUserSettings, redis.ActionUpdate, userSettingsPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("user_id", userID).Msg("Échec du Write-Behind lors de la mise à jour des notifications")
		return user_settings_models.UpdateNotificationsOutput{}, numan_error.NewInternal()
	}

	return user_settings_models.UpdateNotificationsOutput{
		UserSettingsUpdateAt: userSettingsPayload.UpdatedAt,
	}, nil
}
