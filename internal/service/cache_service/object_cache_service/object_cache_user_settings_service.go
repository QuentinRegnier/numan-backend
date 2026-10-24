package object_cache_service

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	goredis "github.com/go-redis/redis/v8"
)

// normalizeUserSettings assainit les réglages historiques (ex: ancien mode visibilité privée 3).
func normalizeUserSettings(s *user_settings_models.UserSettingsPayload) {
	if s.Privacy.PostVisibilityDefault < variables.PostVisibilityAll || s.Privacy.PostVisibilityDefault > variables.PostVisibilityFriend {
		s.Privacy.PostVisibilityDefault = variables.PostVisibilityFriend
	}
}

// ############################################################################
// # SERVICE : OBJECT CACHE (PARAMÈTRES UTILISATEUR LFU)
// ############################################################################

// getUserSettings lit uniquement depuis la RAM L1 (O(1)).
func getUserSettings(ctx context.Context, userID int64) (user_settings_models.UserSettingsPayload, error) {
	var userSettingsPayload user_settings_models.UserSettingsPayload

	// On utilise l'ID de l'utilisateur comme clé car c'est une relation 1-to-1 stricte
	errRedis := redis.UserSettings.GetObject(ctx, userID, &userSettingsPayload)
	if errRedis != nil {
		if errors.Is(errRedis, goredis.Nil) {
			return user_settings_models.UserSettingsPayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Paramètres utilisateur introuvables dans le cache.", errRedis)
		}
		return user_settings_models.UserSettingsPayload{}, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de récupérer les paramètres utilisateur depuis l'Object Cache.", errRedis)
	}

	return userSettingsPayload, nil
}

// SetUserSettings sauvegarde l'objet de paramètres en RAM L1.
func SetUserSettings(ctx context.Context, userSettingsPayload user_settings_models.UserSettingsPayload) error {
	errRedis := redis.UserSettings.SetObject(ctx, userSettingsPayload.UserID, userSettingsPayload)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de sauvegarder les paramètres utilisateur dans l'Object Cache.",
			errRedis,
		)
	}
	return nil
}

// GetUserSettingsCascade tente le L1, puis le L2 (Mongo), puis le L3 (Postgres), et réhydrate la RAM.
func GetUserSettingsCascade(ctx context.Context, userID int64) (user_settings_models.UserSettingsPayload, error) {

	// ── ÉTAPE 1 : TENTATIVE L1 (REDIS RAM) ──────────────────────────────────
	cachedSettings, errL1 := getUserSettings(ctx, userID)
	if errL1 == nil && cachedSettings.ID != 0 {
		normalizeUserSettings(&cachedSettings)
		return cachedSettings, nil
	}

	// ── ÉTAPE 2 : TENTATIVE L2 (MONGODB WARM STORAGE) ────────────────────────
	var mongoSettingsPayload user_settings_models.UserSettingsPayload
	mongoDocumentsList, errMongo := mongo.UserSettings.Get(map[string]any{"user_id": userID}, nil)

	if errMongo != nil {
		numan_log.Warn(ctx).Err(errMongo).Int64("user_id", userID).Msg("Échec L2 lors de la récupération des paramètres utilisateur")
	} else if len(mongoDocumentsList) > 0 {
		if errStruct := pkg.ToStruct(mongoDocumentsList[0], &mongoSettingsPayload); errStruct == nil {
			normalizeUserSettings(&mongoSettingsPayload)
			if errSet := SetUserSettings(ctx, mongoSettingsPayload); errSet != nil {
				numan_log.Warn(ctx).Err(errSet).Int64("user_id", userID).Action(numan_log.ActionHydrate).Msg("Échec de la réhydratation L1 des paramètres utilisateur depuis L2")
			}
			return mongoSettingsPayload, nil
		} else {
			numan_log.Warn(ctx).Err(errStruct).Int64("user_id", userID).Msg("Échec de conversion des paramètres utilisateur L2")
		}
	}

	// ── ÉTAPE 3 : TENTATIVE L3 (POSTGRESQL COLD STORAGE) ────────────────────
	postgresSettings, errPg := postgres.FuncLoadUserSettings(ctx, userID)
	if errPg != nil {
		// Error already wrapped by FuncLoadUserSettings; propagate it without wrapping again.
		return user_settings_models.UserSettingsPayload{}, errPg
	}

	if postgresSettings.ID != 0 {
		normalizeUserSettings(&postgresSettings)

		// A. Réhydratation L1 (Redis - Immédiate en RAM)
		if errSet := SetUserSettings(ctx, postgresSettings); errSet != nil {
			numan_log.Warn(ctx).Err(errSet).Int64("user_id", userID).Action(numan_log.ActionHydrate).Msg("Échec de la réhydratation L1 des paramètres utilisateur depuis L3")
		}

		// B. Réhydratation L2 (Mongo - Asynchrone via les workers de la file d'attente)
		go func(payloadToSync user_settings_models.UserSettingsPayload) {
			backgroundCtx := context.Background() // Contexte détaché de la requête HTTP
			if err := redis.EnqueueDB(backgroundCtx, payloadToSync.ID, payloadToSync.UserID, redis.EntityUserSettings, redis.ActionUpdate, payloadToSync, redis.TargetMongo); err != nil {
				numan_log.Warn(backgroundCtx).Err(err).Entity("user_settings", payloadToSync.ID).Action(numan_log.ActionSync).Msg("Échec de la mise en file d'attente L3->L2 des paramètres utilisateur")
			}
		}(postgresSettings)

		return postgresSettings, nil
	}

	return user_settings_models.UserSettingsPayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Paramètres utilisateur introuvables.", nil)
}
