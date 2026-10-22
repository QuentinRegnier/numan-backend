package object_cache_service

import (
	"context"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
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
		return user_settings_models.UserSettingsPayload{}, errRedis
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

	if errMongo == nil && len(mongoDocumentsList) > 0 {
		if errStruct := pkg.ToStruct(mongoDocumentsList[0], &mongoSettingsPayload); errStruct == nil {
			normalizeUserSettings(&mongoSettingsPayload)
			_ = SetUserSettings(ctx, mongoSettingsPayload) // Réhydratation L1 silencieuse
			return mongoSettingsPayload, nil
		}
	}

	// ── ÉTAPE 3 : TENTATIVE L3 (POSTGRESQL COLD STORAGE) ────────────────────
	postgresSettings, errPg := postgres.FuncLoadUserSettings(ctx, userID)
	if errPg != nil {
		return user_settings_models.UserSettingsPayload{}, errPg
	}

	if postgresSettings.ID != 0 {
		normalizeUserSettings(&postgresSettings)

		// A. Réhydratation L1 (Redis - Immédiate en RAM)
		_ = SetUserSettings(ctx, postgresSettings)

		// B. Réhydratation L2 (Mongo - Asynchrone via les workers de la file d'attente)
		go func(payloadToSync user_settings_models.UserSettingsPayload) {
			backgroundCtx := context.Background() // Contexte détaché de la requête HTTP
			_ = redis.EnqueueDB(backgroundCtx, payloadToSync.ID, payloadToSync.UserID, redis.EntityUserSettings, redis.ActionUpdate, payloadToSync, redis.TargetMongo)
		}(postgresSettings)

		return postgresSettings, nil
	}

	return user_settings_models.UserSettingsPayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Paramètres utilisateur introuvables.", nil)
}
