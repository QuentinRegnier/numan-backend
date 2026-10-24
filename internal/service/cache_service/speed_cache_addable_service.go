package cache_service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/vmihailenco/msgpack/v5"
)

// ############################################################################
// # SERVICE : SPEED CACHE (UTILISATEURS AJOUTABLES)
// ############################################################################

// GetAddableUsersFromSpeedCache résout le sous-cache ZSET (Ami > Abonné + Lexicographique) en O(log N).
func GetAddableUsersFromSpeedCache(ctx context.Context, callerID int64, limit int64, offset int64, forceRefresh bool) ([]lite_models.UserLiteRequest, error) {

	zsetTargetKey := redis.SpeedAddable.Key(callerID)

	// ── ÉTAPE 1 : GESTION DU RAFRAÎCHISSEMENT FORCÉ ─────────────────────────
	if forceRefresh {
		errDel := redis.Del(ctx, zsetTargetKey)
		if errDel != nil {
			numan_log.Warn(ctx).Err(errDel).Int64("user_id", callerID).Msg("Impossible de purger le ZSET AddableUsers")
		}
	}

	isZsetPresent, errExists := redis.Exists(ctx, zsetTargetKey)
	if errExists != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de vérifier le cache des utilisateurs ajoutables.",
			errExists,
		)
	}

	// ── ÉTAPE 2 : CACHE MISS (RECONSTRUCTION INTELLIGENTE) ──────────────────
	if !isZsetPresent {
		addableRelationsList, errPg := postgres.FuncLoadAddableRelations(ctx, callerID)
		if errPg != nil {
			// Error already wrapped by FuncLoadAddableRelations; propagate it without wrapping again.
			return nil, errPg
		}

		if len(addableRelationsList) > 0 {
			var extractedTargetIDs []int64
			relationStateMap := make(map[int64]int)

			for _, relation := range addableRelationsList {
				extractedTargetIDs = append(extractedTargetIDs, relation.TargetID)
				relationStateMap[relation.TargetID] = relation.State
			}

			// Récupération des données compressées L1 (MGET natif du manager)
			multiGetResult, errMGet := redis.UsersLite.GetMany(ctx, extractedTargetIDs)
			if errMGet != nil {
				numan_log.Warn(ctx).Err(errMGet).Msg("Échec MGET lors de la reconstruction SpeedAddable")
			} else {
				for _, targetID := range extractedTargetIDs {
					binaryData, isFound := multiGetResult.Found[targetID]
					if !isFound {
						continue
					}

					var userLitePayload lite_models.UserLiteRequest
					if errUnmarshal := msgpack.Unmarshal(binaryData, &userLitePayload); errUnmarshal == nil {

						// ASTUCE DE TRI :
						// State 2 (Ami) devient 0. State 1 (Abonné) devient 1.
						// Résultat : "0_alice_123" arrivera toujours avant "1_bob_456" dans l'ordre Lexicographique !
						invertedStateValue := variables.RelationStateFriend - relationStateMap[targetID]
						lexicographicMemberString := fmt.Sprintf("%d_%s_%d", invertedStateValue, strings.ToLower(userLitePayload.Username), userLitePayload.ID)

						err := redis.ZAdd(ctx, zsetTargetKey, 0, lexicographicMemberString)
						if err != nil {
							numan_log.Warn(ctx).Err(err).Msg("Échec ZADD lors de la reconstruction SpeedAddable")
							return nil, numan_error.NewAppError(
								http.StatusInternalServerError,
								numan_error.CodeCacheError,
								"Impossible de reconstruire le cache des utilisateurs ajoutables.",
								err,
							)
						}
					} else {
						numan_log.Warn(ctx).Err(errUnmarshal).Msg("Échec de désérialisation MessagePack d'un utilisateur ajoutable")
					}
				}
			}

			// TTL Glissant de 5 minutes
			err := redis.Expire(ctx, zsetTargetKey, 5*time.Minute)
			if err != nil {
				numan_log.Warn(ctx).Err(err).Msg("Échec Expire lors de la reconstruction SpeedAddable")
				return nil, numan_error.NewAppError(
					http.StatusInternalServerError,
					numan_error.CodeCacheError,
					"Impossible de reconstruire le cache des utilisateurs ajoutables.",
					err,
				)
			}

		} else {
			// Injection du Marqueur de vide pour éviter le Cache-Hole
			err := redis.ZAdd(ctx, zsetTargetKey, 0, variables.SpeedCacheEmptyMarker)
			if err != nil {
				numan_log.Warn(ctx).Err(err).Msg("Échec ZADD du marqueur vide lors de la reconstruction SpeedAddable")
				return nil, numan_error.NewAppError(
					http.StatusInternalServerError,
					numan_error.CodeCacheError,
					"Impossible de reconstruire le cache des utilisateurs ajoutables.",
					err,
				)
			}
			err = redis.Expire(ctx, zsetTargetKey, 5*time.Minute)
			if err != nil {
				numan_log.Warn(ctx).Err(err).Msg("Échec Expire lors de la reconstruction SpeedAddable")
				return nil, numan_error.NewAppError(
					http.StatusInternalServerError,
					numan_error.CodeCacheError,
					"Impossible de reconstruire le cache des utilisateurs ajoutables.",
					err,
				)
			}
		}
	}

	// ── ÉTAPE 3 : LECTURE PAGINÉE (O(log(N))) ───────────────────────────────
	memberStringsList, errZRange := redis.ZRange(ctx, zsetTargetKey, offset, offset+limit-1)
	if errZRange != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de lire les utilisateurs ajoutables depuis le cache.",
			errZRange,
		)
	}

	var parsedFinalIDs []int64
	for _, memberString := range memberStringsList {
		if memberString == variables.SpeedCacheEmptyMarker {
			continue
		}

		memberParts := strings.Split(memberString, "_")
		if len(memberParts) == 3 {
			if parsedID := pkg.ParseInt64(memberParts[2]); parsedID != 0 {
				parsedFinalIDs = append(parsedFinalIDs, parsedID)
			}
		}
	}

	if len(parsedFinalIDs) == 0 {
		return []lite_models.UserLiteRequest{}, nil
	}

	// ── ÉTAPE 4 : HYDRATATION FINALE (MGET O(1)) ────────────────────────────
	finalGetResult, errFinalMGet := redis.UsersLite.GetMany(ctx, parsedFinalIDs)
	if errFinalMGet != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de charger les détails des utilisateurs ajoutables depuis le cache.",
			errFinalMGet,
		)
	}

	var hydratedUsersList []lite_models.UserLiteRequest
	for _, finalID := range parsedFinalIDs {
		if binaryData, isFound := finalGetResult.Found[finalID]; isFound {
			var userLitePayload lite_models.UserLiteRequest
			if errUnmarshal := msgpack.Unmarshal(binaryData, &userLitePayload); errUnmarshal == nil {
				hydratedUsersList = append(hydratedUsersList, userLitePayload)
			} else {
				numan_log.Warn(ctx).Err(errUnmarshal).Msg("Échec de désérialisation MessagePack d'un utilisateur ajoutable")
			}
		}
	}

	return hydratedUsersList, nil
}
