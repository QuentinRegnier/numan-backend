package cache_service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	goredis "github.com/go-redis/redis/v8"
	"github.com/vmihailenco/msgpack/v5"
)

// ############################################################################
// # SERVICE : SPEED CACHE (PROFILS UTILISATEURS ALLÉGÉS)
// ############################################################################

// storeUserLiteInSpeedCache sauvegarde directement un objet UserLiteRequest
// et met à jour l'index Lexicographique pour la recherche et l'autocomplétion.
func storeUserLiteInSpeedCache(ctx context.Context, userLitePayload lite_models.UserLiteRequest) error {

	// 1. Insertion dans l'index lexicographique ("pseudo:id")
	lexicographicValue := fmt.Sprintf("%s:%d", strings.ToLower(userLitePayload.Username), userLitePayload.ID)
	errLex := redis.UsersLex.ZAdd(ctx, variables.LexicographicGlobalKey, 0, lexicographicValue)
	if errLex != nil {
		numan_log.Warn(ctx).Err(errLex).Msg("Échec de l'indexation lexicographique d'un utilisateur")
	}

	// 2. Sauvegarde dans l'Object Cache Rapide L1
	errSet := redis.UsersLite.SetObject(ctx, userLitePayload.ID, userLitePayload)
	if errSet != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de sauvegarder l'utilisateur dans le cache.",
			errSet,
		)
	}

	return nil
}

// AddUserToSpeedCache insère un nouvel utilisateur (à la création de compte) dans
// l'index de recherche et le store SPEED cache L1.
func AddUserToSpeedCache(ctx context.Context, userPayload auth_models.UserPayload, settingsPayload user_settings_models.UserSettingsPayload) error {

	lexicographicValue := fmt.Sprintf("%s:%d", strings.ToLower(userPayload.Username), userPayload.ID)
	if errLex := redis.UsersLex.ZAdd(ctx, variables.LexicographicGlobalKey, 0, lexicographicValue); errLex != nil {
		numan_log.Warn(ctx).Err(errLex).Int64("user_id", userPayload.ID).Msg("Échec de l'indexation lexicographique d'un utilisateur")
	}

	userBadges := userPayload.Badges
	if userBadges == nil {
		userBadges = lite_models.UserBadges{}
	}

	constructedUserLite := lite_models.UserLiteRequest{
		ID:                     userPayload.ID,
		Username:               userPayload.Username,
		FirstName:              userPayload.FirstName,
		LastName:               userPayload.LastName,
		ProfilePictureID:       userPayload.ProfilePictureID,
		Bio:                    userPayload.Bio,
		Grade:                  userPayload.Grade,
		Badges:                 userBadges,
		ConversationPermission: settingsPayload.Privacy.ConversationPermission,
		AddGroupPermission:     settingsPayload.Privacy.AddGroupPermission,
		HideConnections:        settingsPayload.Privacy.HideConnections,
		ShowOnlineStatus:       settingsPayload.Privacy.ShowOnlineStatus,
		AllowTagging:           settingsPayload.Privacy.AllowTagging,
		AllowMentions:          settingsPayload.Privacy.AllowMentions,
	}

	errSet := redis.UsersLite.SetObject(ctx, userPayload.ID, constructedUserLite)
	if errSet != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'ajouter l'utilisateur au cache.",
			errSet,
		)
	}

	return nil
}

// UpdateUserSpeedCachePrivacy synchronise les changements de confidentialité directement
// en RAM pour un effet immédiat sur les autres utilisateurs.
func UpdateUserSpeedCachePrivacy(ctx context.Context, userID int64, conversationPerm int, addGroupPerm int, hideConnectionsPerm int) error {
	var userLitePayload lite_models.UserLiteRequest

	errGet := redis.UsersLite.GetObject(ctx, userID, &userLitePayload)
	if errGet != nil && !errors.Is(errGet, goredis.Nil) {
		numan_log.Warn(ctx).Err(errGet).Int64("user_id", userID).Msg("Erreur Redis lors de la lecture du profil allégé pour mise à jour de confidentialité")
	}
	if errGet == nil && userLitePayload.ID != 0 {
		userLitePayload.ConversationPermission = conversationPerm
		userLitePayload.AddGroupPermission = addGroupPerm
		userLitePayload.HideConnections = hideConnectionsPerm

		errSet := redis.UsersLite.SetObject(ctx, userID, userLitePayload)
		if errSet != nil {
			return numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeCacheError,
				"Impossible de mettre à jour la confidentialité de l'utilisateur dans le cache.",
				errSet,
			)
		}
	}
	return nil
}

// SearchUserByPrefix recherche des utilisateurs via l'autocomplétion.
func SearchUserByPrefix(ctx context.Context, searchPrefix string, searchOffset, searchLimit int64) ([]lite_models.UserLiteRequest, error) {

	// 1. Recherche ultra-rapide dans l'index lexicographique
	lexicographicResultsList, errLex := redis.UsersLex.ZRangeByLex(ctx, variables.LexicographicGlobalKey, strings.ToLower(searchPrefix), searchOffset, searchLimit)
	if errLex != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de rechercher les utilisateurs dans le cache.",
			errLex,
		)
	}

	if len(lexicographicResultsList) == 0 {
		return []lite_models.UserLiteRequest{}, nil
	}

	// 2. Extraction des IDs
	var extractedIDsList []int64
	for _, lexString := range lexicographicResultsList {
		// Le format stocké est "pseudo:id"
		lexParts := strings.Split(lexString, ":")
		if len(lexParts) == 2 {
			if parsedID := pkg.ParseInt64(lexParts[1]); parsedID != 0 {
				extractedIDsList = append(extractedIDsList, parsedID)
			}
		}
	}

	// 3. Hydratation massive via MGET sur la collection UsersLite
	multiGetResult, errMGet := redis.UsersLite.GetMany(ctx, extractedIDsList)
	if errMGet != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'hydrater les utilisateurs depuis le cache.",
			errMGet,
		)
	}

	var hydratedUsersList []lite_models.UserLiteRequest

	// 4. On boucle sur l'array originel pour conserver l'ordre alphabétique
	for _, requestedID := range extractedIDsList {
		if binaryData, isFound := multiGetResult.Found[requestedID]; isFound {
			var userLitePayload lite_models.UserLiteRequest
			if errUnmarshal := msgpack.Unmarshal(binaryData, &userLitePayload); errUnmarshal == nil {
				hydratedUsersList = append(hydratedUsersList, userLitePayload)
			} else {
				numan_log.Warn(ctx).Err(errUnmarshal).Int64("user_id", requestedID).Msg("Échec de désérialisation MessagePack d'un utilisateur")
			}
		}
	}

	// Si un ID manque dans le cache, on l'ignore silencieusement.
	// Pour de l'autocomplétion, la vitesse prime sur l'exhaustivité absolue.
	return hydratedUsersList, nil
}

// GetUserLite récupère l'empreinte minimale d'un utilisateur depuis le SPEED Cache (L1)
// avec un fallback en cascade L2 (MongoDB) -> L3 (PostgreSQL) et réhydratation automatique.
func GetUserLite(ctx context.Context, userID int64) (lite_models.UserLiteRequest, error) {
	var userLitePayload lite_models.UserLiteRequest

	// ── ÉTAPE 1 : TENTATIVE L1 (SPEED CACHE) ────────────────────────────────
	errL1 := redis.UsersLite.GetObject(ctx, userID, &userLitePayload)
	if errL1 == nil {
		return userLitePayload, nil
	}

	// ── ÉTAPE 2 : FALLBACK L2 (WARM STORAGE MONGODB) ────────────────────────
	userFromMongo, errMongo := mongo.MongoLoadUser(ctx, userID, "", "", "")
	if errMongo == nil {
		// Récupération des paramètres de confidentialité L1
		settingsPayload, errSettings := object_cache_service.GetUserSettingsCascade(ctx, userID)
		if errSettings != nil {
			numan_log.Warn(ctx).Err(errSettings).Int64("user_id", userID).Msg("Impossible de récupérer les paramètres utilisateur lors de l'hydratation UserLite L2")
		}

		// Réhydratation L1 synchrone avec les deux objets combinés
		if errAdd := AddUserToSpeedCache(ctx, userFromMongo, settingsPayload); errAdd != nil {
			numan_log.Warn(ctx).Err(errAdd).Int64("user_id", userID).Action(numan_log.ActionHydrate).Msg("Échec de la réhydratation L1 UserLite depuis L2")
		}

		userBadges := userFromMongo.Badges
		if userBadges == nil {
			userBadges = lite_models.UserBadges{}
		}

		return lite_models.UserLiteRequest{
			ID:                     userFromMongo.ID,
			Username:               userFromMongo.Username,
			FirstName:              userFromMongo.FirstName,
			LastName:               userFromMongo.LastName,
			ProfilePictureID:       userFromMongo.ProfilePictureID,
			Bio:                    userFromMongo.Bio,
			Grade:                  userFromMongo.Grade,
			Badges:                 userBadges,
			ConversationPermission: settingsPayload.Privacy.ConversationPermission,
			AddGroupPermission:     settingsPayload.Privacy.AddGroupPermission,
			HideConnections:        settingsPayload.Privacy.HideConnections,
			ShowOnlineStatus:       settingsPayload.Privacy.ShowOnlineStatus,
			AllowTagging:           settingsPayload.Privacy.AllowTagging,
			AllowMentions:          settingsPayload.Privacy.AllowMentions,
		}, nil
	}

	// ── ÉTAPE 3 : FALLBACK ABSOLU L3 (COLD STORAGE POSTGRESQL) ──────────────
	userFromPg, errPg := postgres.FuncLoadUser(ctx, userID, "", "", "")
	if errPg != nil {
		// Error already wrapped by FuncLoadUser; propagate it without wrapping again.
		return userLitePayload, errPg
	}
	if userFromPg.ID != 0 {

		// PROMOTION L3 -> L2 (Write-Behind)
		go func(promotedUser auth_models.UserPayload) {
			backgroundCtx := context.Background()
			if err := redis.EnqueueDB(backgroundCtx, promotedUser.ID, 0, redis.EntityUser, redis.ActionUpdate, promotedUser, redis.TargetMongo); err != nil {
				numan_log.Warn(backgroundCtx).Err(err).Entity("user", promotedUser.ID).Action(numan_log.ActionSync).Msg("Échec de la mise en file d'attente L3->L2 de l'utilisateur")
			}
		}(userFromPg)

		// PROMOTION L3 -> L1
		settingsPayload, errSettings := object_cache_service.GetUserSettingsCascade(ctx, userID)
		if errSettings != nil {
			numan_log.Warn(ctx).Err(errSettings).Int64("user_id", userID).Msg("Impossible de récupérer les paramètres utilisateur lors de l'hydratation UserLite L3")
		}
		if errAdd := AddUserToSpeedCache(ctx, userFromPg, settingsPayload); errAdd != nil {
			numan_log.Warn(ctx).Err(errAdd).Int64("user_id", userID).Action(numan_log.ActionHydrate).Msg("Échec de la réhydratation L1 UserLite depuis L3")
		}

		pgBadges := userFromPg.Badges
		if pgBadges == nil {
			pgBadges = lite_models.UserBadges{}
		}

		return lite_models.UserLiteRequest{
			ID:                     userFromPg.ID,
			Username:               userFromPg.Username,
			FirstName:              userFromPg.FirstName,
			LastName:               userFromPg.LastName,
			ProfilePictureID:       userFromPg.ProfilePictureID,
			Bio:                    userFromPg.Bio,
			Grade:                  userFromPg.Grade,
			Badges:                 pgBadges,
			ConversationPermission: settingsPayload.Privacy.ConversationPermission,
			AddGroupPermission:     settingsPayload.Privacy.AddGroupPermission,
			HideConnections:        settingsPayload.Privacy.HideConnections,
			ShowOnlineStatus:       settingsPayload.Privacy.ShowOnlineStatus,
			AllowTagging:           settingsPayload.Privacy.AllowTagging,
			AllowMentions:          settingsPayload.Privacy.AllowMentions,
		}, nil
	}

	return userLitePayload, numan_error.NewNotFound(numan_error.CodeNotFound, "L'utilisateur est introuvable.", nil)
}
