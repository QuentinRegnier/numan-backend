package cache_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : GESTION DE L'IDEMPOTENCE DES INTERACTIONS (L1 + RÉHYDRATATION L2/L3)
// ############################################################################

// likesRehydrationCap borne le nombre de likers réhydratés en une seule passe
// depuis L2/L3 afin de protéger la RAM et la latence sur les contenus très viraux.
// Au-delà de ce plafond, une vérification ponctuelle exacte L2/L3 prend le relais.
const likesRehydrationCap int64 = 5000

// getIdempotencyCollection retourne le Set Redis L1 approprié selon le type de cible.
func getIdempotencyCollection(targetEntityType int) *redis.Collection {
	switch targetEntityType {
	case variables.LikeTargetTypePost: // 0
		return redis.PostLikesSet
	case variables.LikeTargetTypeComment: // 1
		return redis.CommentLikesSet
	default:
		// Sécurité absolue : on ne retourne rien si le type est inconnu
		return nil
	}
}

// hasUserLikedInStorage vérifie l'existence exacte d'un like en cascade L2 Mongo -> L3 Postgres.
func hasUserLikedInStorage(ctx context.Context, targetEntityType int, targetID int64, userID int64) bool {
	if mongo.Likes != nil {
		if liked, err := mongo.MongoHasUserLiked(ctx, targetEntityType, targetID, userID); err == nil && liked {
			return true
		}
	}

	likes, err := postgres.FuncLoadLikes(ctx, targetEntityType, targetID, userID, 1, 0)
	return err == nil && len(likes) > 0
}

// rehydrateLikesSetFromStorage recharge le SET Redis d'idempotence depuis L2 Mongo
// (avec repli L3 PostgreSQL) lorsqu'une clé a expiré après son LongTTL (30j).
// Retourne true si le résultat atteint likesRehydrationCap (troncature potentielle).
func rehydrateLikesSetFromStorage(ctx context.Context, col *redis.Collection, targetEntityType int, targetID int64) bool {
	var userIDs []int64

	if mongo.Likes != nil {
		if ids, err := mongo.MongoGetTargetLikes(ctx, targetEntityType, targetID, likesRehydrationCap); err == nil && len(ids) > 0 {
			userIDs = ids
		}
	}

	if len(userIDs) == 0 {
		if likes, err := postgres.FuncLoadLikes(ctx, targetEntityType, targetID, 0, likesRehydrationCap, 0); err == nil && len(likes) > 0 {
			userIDs = make([]int64, 0, len(likes))
			for _, l := range likes {
				userIDs = append(userIDs, l.UserID)
			}
		}
	}

	if len(userIDs) == 0 {
		return false
	}

	members := make([]any, len(userIDs))
	for i, uid := range userIDs {
		members[i] = uid
	}

	if err := col.SAdd(ctx, targetID, members...); err != nil {
		numan_log.Warn(ctx).Err(err).Int("target_type", targetEntityType).Int64("target_id", targetID).Msg("Échec de la réhydratation L1 du set d'idempotence des likes")
	} else {
		_ = col.RefreshTTL(ctx, targetID)
	}

	return int64(len(userIDs)) >= likesRehydrationCap
}

// TryAddLikeIdempotency gère l'idempotence pour l'ajout de Likes de manière thread-safe (O(1) à chaud).
// Sur cache miss L1 (clé expirée après LongTTL), réhydrate le SET depuis L2 Mongo -> L3 Postgres
// afin d'éviter les doubles incréments de LikeCount et les notifications dupliquées.
// Retourne true si l'ajout est un succès (l'utilisateur n'avait pas encore liké).
func TryAddLikeIdempotency(ctx context.Context, targetEntityType int, targetID int64, userID int64) bool {
	redisCollection := getIdempotencyCollection(targetEntityType)
	if redisCollection == nil {
		numan_log.Warn(ctx).Int("target_type", targetEntityType).Int64("target_id", targetID).Msg("Type d'entité cible inconnu pour l'idempotence des likes")
		return false
	}

	exists, errExists := redisCollection.Exists(ctx, targetID)
	truncated := false
	if errExists == nil && !exists {
		truncated = rehydrateLikesSetFromStorage(ctx, redisCollection, targetEntityType, targetID)
		if truncated && hasUserLikedInStorage(ctx, targetEntityType, targetID, userID) {
			_ = redisCollection.SAdd(ctx, targetID, userID)
			_ = redisCollection.RefreshTTL(ctx, targetID)
			return false
		}
	}

	elementsAddedCount, errRedis := redisCollection.SAddCount(ctx, targetID, userID)
	if errRedis != nil {
		numan_log.Warn(ctx).Err(errRedis).Int("target_type", targetEntityType).Int64("target_id", targetID).Int64("user_id", userID).Msg("Échec Redis lors de la vérification d'idempotence d'ajout de like")
		return false
	}

	_ = redisCollection.RefreshTTL(ctx, targetID)

	if elementsAddedCount == 0 {
		return false
	}

	// Si le SET existait déjà et atteint le plafond de réhydratation (post très viral),
	// on vérifie en L2/L3 avant de valider un nouveau like pour exclure tout doublon.
	if exists && !truncated {
		if card, errCard := redisCollection.SCard(ctx, targetID); errCard == nil && card > likesRehydrationCap {
			if hasUserLikedInStorage(ctx, targetEntityType, targetID, userID) {
				return false
			}
		}
	}

	return true
}

// TryRemoveLikeIdempotency gère la suppression d'idempotence pour les Likes (O(1) à chaud).
// Si la clé L1 a expiré après LongTTL, réhydrate depuis L2 Mongo -> L3 Postgres pour garantir
// qu'un utilisateur puisse toujours retirer son like même plusieurs mois plus tard.
// Retourne true si le retrait est un succès (l'utilisateur avait bien liké).
func TryRemoveLikeIdempotency(ctx context.Context, targetEntityType int, targetID int64, userID int64) bool {
	redisCollection := getIdempotencyCollection(targetEntityType)
	if redisCollection == nil {
		numan_log.Warn(ctx).Int("target_type", targetEntityType).Int64("target_id", targetID).Msg("Type d'entité cible inconnu pour l'idempotence de retrait de like")
		return false
	}

	elementsRemovedCount, errRedis := redisCollection.SRemCount(ctx, targetID, userID)
	if errRedis != nil {
		numan_log.Warn(ctx).Err(errRedis).Int("target_type", targetEntityType).Int64("target_id", targetID).Int64("user_id", userID).Msg("Échec Redis lors de la vérification d'idempotence de retrait de like")
		return false
	}

	if elementsRemovedCount > 0 {
		_ = redisCollection.RefreshTTL(ctx, targetID)
		return true
	}

	// Chemin froid : la clé L1 a peut-être expiré après LongTTL (30j)
	exists, errExists := redisCollection.Exists(ctx, targetID)
	if errExists != nil {
		return false
	}

	if !exists {
		truncated := rehydrateLikesSetFromStorage(ctx, redisCollection, targetEntityType, targetID)
		retriedRemoved, errRetry := redisCollection.SRemCount(ctx, targetID, userID)
		if errRetry == nil && retriedRemoved > 0 {
			_ = redisCollection.RefreshTTL(ctx, targetID)
			return true
		}
		if truncated && hasUserLikedInStorage(ctx, targetEntityType, targetID, userID) {
			return true
		}
		return false
	}

	// Si le SET est présent mais potentiellement tronqué (> likesRehydrationCap), vérifier en L2/L3
	if card, errCard := redisCollection.SCard(ctx, targetID); errCard == nil && card >= likesRehydrationCap {
		return hasUserLikedInStorage(ctx, targetEntityType, targetID, userID)
	}

	return false
}
