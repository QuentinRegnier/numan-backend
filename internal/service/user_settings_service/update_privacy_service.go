package user_settings_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : MISE À JOUR DES PARAMÈTRES DE CONFIDENTIALITÉ
// ############################################################################

// UpdatePrivacy modifie les paramètres de confidentialité, synchronise le Speed Cache
// et délègue la sauvegarde au système de Write-Behind.
func UpdatePrivacy(ctx context.Context, userID int64, input user_settings_models.UpdatePrivacyInput) (user_settings_models.UpdatePrivacyOutput, error) {
	// ── ÉTAPE 0 : VÉRIFICATION DE LA VISIBILITÉ PAR DÉFAUT ──────────────────
	if input.PostVisibilityDefault < variables.PostVisibilityAll || input.PostVisibilityDefault > variables.PostVisibilityFriend {
		return user_settings_models.UpdatePrivacyOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Visibilité par défaut des posts invalide.", nil)
	}

	// ── ÉTAPE 1 : RÉCUPÉRATION DES PARAMÈTRES (CASCADE L1 -> L2 -> L3) ──────
	userSettingsPayload, errCache := object_cache_service.GetUserSettingsCascade(ctx, userID)
	if errCache != nil || userSettingsPayload.ID == 0 {
		return user_settings_models.UpdatePrivacyOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Paramètres de l'utilisateur introuvables.", errCache)
	}

	// ── ÉTAPE 2 : REMPLACEMENT INTÉGRAL (MÉTHODE PUT) ───────────────────────
	userSettingsPayload.Privacy.ProfileVisibility = input.ProfileVisibility
	userSettingsPayload.Privacy.PostVisibilityDefault = input.PostVisibilityDefault
	userSettingsPayload.Privacy.ConversationPermission = input.ConversationPermission
	userSettingsPayload.Privacy.AddGroupPermission = input.AddGroupPermission
	userSettingsPayload.Privacy.AllowTagging = input.AllowTagging
	userSettingsPayload.Privacy.AllowMentions = input.AllowMentions
	userSettingsPayload.Privacy.ShowOnlineStatus = input.ShowOnlineStatus
	userSettingsPayload.Privacy.SendReadReceipts = input.SendReadReceipts
	userSettingsPayload.Privacy.SearchByEmailPhone = input.SearchByEmailPhone
	userSettingsPayload.Privacy.ShowLocation = input.ShowLocation
	userSettingsPayload.Privacy.HideConnections = input.HideConnections

	userSettingsPayload.UpdatedAt = domain.NowMillis()

	// ── ÉTAPE 3 : MISE À JOUR IMMÉDIATE DU CACHE L1 (OBJECT CACHE) ──────────
	if errSet := object_cache_service.SetUserSettings(ctx, userSettingsPayload); errSet != nil {
		numan_log.Warn(ctx).Err(errSet).Int64("user_id", userID).Msg("Impossible de mettre à jour les paramètres de confidentialité dans le cache L1")
	}

	// ── ÉTAPE 4 : MISE À JOUR SYNCHRONE DU SPEED CACHE ──────────────────────
	// Indispensable pour que les autres utilisateurs puissent voir la mise à jour
	// des permissions instantanément sans devoir charger l'objet entier.
	_ = cache_service.UpdateUserSpeedCachePrivacy(
		ctx,
		userSettingsPayload.UserID,
		userSettingsPayload.Privacy.ConversationPermission,
		userSettingsPayload.Privacy.AddGroupPermission,
		userSettingsPayload.Privacy.HideConnections,
	)

	// ── ÉTAPE 5 : NOTIFICATION TEMPS RÉEL (WEBSOCKET) ───────────────────────
	errBroadcast := realtime_service.DistributeToUsers(ctx, variables.NotificationSettingsUpdated, userSettingsPayload, []int64{userID})
	if errBroadcast != nil {
		numan_log.Warn(ctx).Err(errBroadcast).Msg("Échec de la distribution WebSocket pour la mise à jour de confidentialité")
	}

	// ── ÉTAPE 6 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────────
	errQueue := redis.EnqueueDB(ctx, userSettingsPayload.ID, userID, redis.EntityUserSettings, redis.ActionUpdate, userSettingsPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("user_id", userID).Msg("Échec du Write-Behind lors de la mise à jour de la confidentialité")
		return user_settings_models.UpdatePrivacyOutput{}, numan_error.NewInternal()
	}

	return user_settings_models.UpdatePrivacyOutput{
		UserSettingsUpdateAt: userSettingsPayload.UpdatedAt,
	}, nil
}
