package cache_service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/relation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : SPEED CACHE (RELATIONS ET GRAPHE SOCIAL)
// ############################################################################

// RelationValue est le moteur d'accès L1 -> L2 -> L3 pour récupérer l'état
// relationnel exact entre deux utilisateurs.
func RelationValue(ctx context.Context, targetID int64, callerID int64) int {
	callerIDString := strconv.FormatInt(callerID, 10)

	// ── ÉTAPE 1 : VÉRIFICATION RAM L1 (O(1)) ────────────────────────────────
	cachedStateString, errRedis := redis.SpeedRelations.HGet(ctx, targetID, callerIDString).Result()
	if errRedis == nil {
		if relationState, errParse := strconv.Atoi(cachedStateString); errParse == nil {
			return relationState
		}
	}

	// ── ÉTAPE 2 : COLD STORAGE L2 (MONGODB WARM STORAGE) ────────────────────
	relationStateFromMongo, errMongo := mongo.MongoGetRelationState(ctx, callerID, targetID)
	if errMongo == nil {
		if errHSet := redis.SpeedRelations.HSet(ctx, targetID, callerIDString, relationStateFromMongo); errHSet != nil {
			numan_log.Warn(ctx).Err(errHSet).Int64("target_id", targetID).Int64("caller_id", callerID).Msg("Échec de la réhydratation L1 de la relation depuis L2")
		}
		return relationStateFromMongo
	}

	// ── ÉTAPE 3 : SOURCE DE VÉRITÉ L3 (POSTGRESQL) ──────────────────────────
	relationStateFromPg, errPg := postgres.FuncGetRelationState(ctx, callerID, targetID)
	if errPg != nil {
		numan_log.Error(ctx).Err(errPg).Int64("target_id", targetID).Int64("caller_id", callerID).Msg("Erreur L3 RelationValue")
		return variables.RelationStateNone // Zéro-valeur sécurisée par défaut
	}

	if errHSet := redis.SpeedRelations.HSet(ctx, targetID, callerIDString, relationStateFromPg); errHSet != nil {
		numan_log.Warn(ctx).Err(errHSet).Int64("target_id", targetID).Int64("caller_id", callerID).Msg("Échec de la réhydratation L1 de la relation depuis L3")
	}

	// RÉHYDRATATION L2 ASYNCHRONE
	go func(currentState int) {
		backgroundCtx := context.Background()
		relationPayload := relation_models.RelationPayload{
			PrimaryID:   callerID,
			SecondaryID: targetID,
			State:       currentState,
			UpdatedAt:   domain.NowMillis(),
		}
		if err := redis.EnqueueDB(backgroundCtx, 0, targetID, redis.EntityRelation, redis.ActionUpdate, relationPayload, redis.TargetMongo); err != nil {
			numan_log.Warn(backgroundCtx).Err(err).Int64("target_id", targetID).Int64("caller_id", callerID).Action(numan_log.ActionSync).Msg("Échec de la mise en file d'attente L3->L2 de la relation")
		}
	}(relationStateFromPg)

	return relationStateFromPg
}

// UpdateRelationState met à jour l'état de la relation (HSET) ET le tri temporel (ZSET).
func UpdateRelationState(ctx context.Context, targetID int64, callerID int64, newRelationState int, timestampMs int64) error {

	// 1. Maintien du dictionnaire d'accès O(1)
	callerIDString := strconv.FormatInt(callerID, 10)
	targetIDString := strconv.FormatInt(targetID, 10)

	errHSet := redis.SpeedRelations.HSet(ctx, targetID, callerIDString, newRelationState)
	if errHSet != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de mettre à jour la relation dans le cache.",
			errHSet,
		)
	}

	timestampScore := float64(timestampMs)

	// 2. Gestion des ZSETs chronologiques directionnels
	// On nettoie l'ancien état par précaution pour éviter les doublons
	if err := redis.SpeedRelationsIndex.ZRem(ctx, fmt.Sprintf("in:%d:%d", variables.RelationStateFollow, targetID), callerIDString); err != nil {
		numan_log.Warn(ctx).Err(err).Int64("target_id", targetID).Int64("caller_id", callerID).Msg("Échec ZRem de l'index Follow lors de la mise à jour de relation")
	}
	if err := redis.SpeedRelationsIndex.ZRem(ctx, fmt.Sprintf("in:%d:%d", variables.RelationStateFriend, targetID), callerIDString); err != nil {
		numan_log.Warn(ctx).Err(err).Int64("target_id", targetID).Int64("caller_id", callerID).Msg("Échec ZRem de l'index Friend lors de la mise à jour de relation")
	}
	if err := redis.SpeedRelationsIndex.ZRem(ctx, fmt.Sprintf("out:%d:%d", variables.RelationStateBlocked, callerID), targetIDString); err != nil {
		numan_log.Warn(ctx).Err(err).Int64("target_id", targetID).Int64("caller_id", callerID).Msg("Échec ZRem de l'index Blocked lors de la mise à jour de relation")
	}

	// 3. Ajout dans le nouveau ZSET si l'état est actif
	if newRelationState == variables.RelationStateFollow {
		if err := redis.SpeedRelationsIndex.ZAdd(ctx, fmt.Sprintf("in:%d:%d", variables.RelationStateFollow, targetID), timestampScore, callerIDString); err != nil {
			numan_log.Warn(ctx).Err(err).Int64("target_id", targetID).Int64("caller_id", callerID).Msg("Échec ZAdd de l'index Follow lors de la mise à jour de relation")
			return numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeCacheError,
				"Impossible de mettre à jour l'index temporel de la relation dans le cache.",
				err,
			)
		}
	} else if newRelationState == variables.RelationStateFriend {
		if err := redis.SpeedRelationsIndex.ZAdd(ctx, fmt.Sprintf("in:%d:%d", variables.RelationStateFriend, targetID), timestampScore, callerIDString); err != nil {
			numan_log.Warn(ctx).Err(err).Int64("target_id", targetID).Int64("caller_id", callerID).Msg("Échec ZAdd de l'index Friend lors de la mise à jour de relation")
			return numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeCacheError,
				"Impossible de mettre à jour l'index temporel de la relation dans le cache.",
				err,
			)
		}
	} else if newRelationState == variables.RelationStateBlocked {
		if err := redis.SpeedRelationsIndex.ZAdd(ctx, fmt.Sprintf("out:%d:%d", variables.RelationStateBlocked, callerID), timestampScore, targetIDString); err != nil {
			numan_log.Warn(ctx).Err(err).Int64("target_id", targetID).Int64("caller_id", callerID).Msg("Échec ZAdd de l'index Blocked lors de la mise à jour de relation")
			return numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeCacheError,
				"Impossible de mettre à jour l'index temporel de la relation dans le cache.",
				err,
			)
		}
	}

	return nil
}

// GetSpeedRelationsIndex récupère les abonnés en O(log N).
func GetSpeedRelationsIndex(ctx context.Context, userID int64) ([]int64, error) {
	zsetKey := fmt.Sprintf("in:%d:%d", variables.RelationStateFollow, userID)

	followerStringIDs, errRedis := redis.SpeedRelationsIndex.ZRevRange(ctx, zsetKey, 0, -1)
	if errRedis != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de lire l'index des abonnés depuis le cache.",
			errRedis,
		)
	}

	followersIDsList := pkg.ParseInt64List(followerStringIDs)

	return followersIDsList, nil
}

// GetSpeedFriends récupère strictement la liste des amis.
func GetSpeedFriends(ctx context.Context, userID int64) ([]int64, error) {
	zsetKey := fmt.Sprintf("in:%d:%d", variables.RelationStateFriend, userID)

	friendStringIDs, errRedis := redis.SpeedRelationsIndex.ZRevRange(ctx, zsetKey, 0, -1)
	if errRedis != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de lire l'index des amis depuis le cache.",
			errRedis,
		)
	}

	friendsIDsList := pkg.ParseInt64List(friendStringIDs)

	return friendsIDsList, nil
}

// GetFollowerCount retourne le nombre total d'abonnés en O(1).
func GetFollowerCount(ctx context.Context, userID int64) int64 {
	zsetKey := fmt.Sprintf("in:%d:%d", variables.RelationStateFollow, userID)
	count, err := redis.SpeedRelationsIndex.ZCard(ctx, zsetKey)
	if err != nil {
		numan_log.Warn(ctx).Err(err).Int64("user_id", userID).Msg("Échec de la lecture du nombre d'abonnés depuis le cache")
		return 0
	}
	return count
}

// GetRelationsByDirectionPaginatedFromCache récupère les IDs ciblés de manière paginée en O(log N).
func GetRelationsByDirectionPaginatedFromCache(ctx context.Context, primaryID int64, relationState int, searchDirection string, fetchLimit int64, fetchOffset int64) ([]int64, error) {
	var targetZsetKey string

	// Format imposé : in:{state}:{targetID} ou out:{state}:{callerID}
	if searchDirection == "incoming" {
		targetZsetKey = fmt.Sprintf("in:%d:%d", relationState, primaryID)
	} else {
		targetZsetKey = fmt.Sprintf("out:%d:%d", relationState, primaryID)
	}

	targetStringsIDs, errRedis := redis.SpeedRelationsIndex.ZRevRange(ctx, targetZsetKey, int64(fetchOffset), int64(fetchOffset+fetchLimit-1))
	if errRedis != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de lire les relations par direction depuis le cache.",
			errRedis,
		)
	}

	matchedIDsList := pkg.ParseInt64List(targetStringsIDs)

	return matchedIDsList, nil
}
