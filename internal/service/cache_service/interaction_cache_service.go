package cache_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : GESTION DE L'IDEMPOTENCE DES INTERACTIONS
// ############################################################################

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

// TryAddLikeIdempotency gère l'idempotence pour l'ajout de Likes de manière thread-safe (O(1)).
// Retourne true si l'ajout est un succès (l'utilisateur n'avait pas encore liké).
func TryAddLikeIdempotency(ctx context.Context, targetEntityType int, targetID int64, userID int64) bool {
	redisCollection := getIdempotencyCollection(targetEntityType)
	if redisCollection == nil {
		numan_log.Warn(ctx).Int("target_type", targetEntityType).Int64("target_id", targetID).Msg("Type d'entité cible inconnu pour l'idempotence des likes")
		return false
	}

	elementsAddedCount, errRedis := redisCollection.SAddCount(ctx, targetID, userID)
	if errRedis != nil {
		numan_log.Warn(ctx).Err(errRedis).Int("target_type", targetEntityType).Int64("target_id", targetID).Int64("user_id", userID).Msg("Échec Redis lors de la vérification d'idempotence d'ajout de like")
		return false
	}

	return elementsAddedCount > 0
}

// TryRemoveLikeIdempotency gère la suppression d'idempotence pour les Likes (O(1)).
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

	return elementsRemovedCount > 0
}
