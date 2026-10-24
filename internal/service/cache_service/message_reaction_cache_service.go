package cache_service

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	goredis "github.com/go-redis/redis/v8"
)

// ############################################################################
// # SERVICE : CACHE DES RÉACTIONS DE MESSAGERIE (HASH)
// ############################################################################

// GetUserReaction récupère l'emoji spécifique posé par un utilisateur sur un message.
func GetUserReaction(ctx context.Context, messageID int64, userID int64) (string, error) {
	emojiString, errRedis := redis.MessageUserReactions.HGet(ctx, messageID, strconv.FormatInt(userID, 10)).Result()

	if errRedis != nil {
		if errors.Is(errRedis, goredis.Nil) {
			return "", nil
		}
		return "", numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de récupérer la réaction de l'utilisateur depuis le cache.",
			errRedis,
		)
	}

	return emojiString, nil
}

// SetUserReaction sauvegarde en RAM (O(1)) l'emoji choisi par l'utilisateur.
func SetUserReaction(ctx context.Context, messageID int64, userID int64, emojiString string) error {
	errRedis := redis.MessageUserReactions.HSet(ctx, messageID, strconv.FormatInt(userID, 10), emojiString)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'enregistrer la réaction dans le cache.",
			errRedis,
		)
	}
	return nil
}

// DeleteUserReaction supprime la trace de la réaction de l'utilisateur.
func DeleteUserReaction(ctx context.Context, messageID int64, userID int64) error {
	errRedis := redis.MessageUserReactions.HDel(ctx, messageID, strconv.FormatInt(userID, 10))
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de supprimer la réaction du cache.",
			errRedis,
		)
	}
	return nil
}

// IncrementReactionCount modifie le compteur aggloméré d'un emoji spécifique.
func IncrementReactionCount(ctx context.Context, messageID int64, emojiString string, delta int64) error {
	redisKey := redis.MessageReactionCounts.Key(messageID)

	newCounterValue, errRedis := redis.MessageReactionCounts.Client.HIncrBy(ctx, redisKey, emojiString, delta).Result()
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de modifier le compteur de réactions dans le cache.",
			errRedis,
		)
	}

	// Nettoyage automatique RAM si le compteur tombe à zéro ou en négatif pour ne pas polluer le Payload JSON.
	if newCounterValue <= 0 {
		errDel := redis.MessageReactionCounts.Client.HDel(ctx, redisKey, emojiString).Err()
		if errDel != nil {
			numan_log.Warn(ctx).Err(errDel).Msg("Échec du nettoyage HDel après décrémentation à zéro")
		}
	}

	return nil
}

// GetMessageReactionCounts récupère l'ensemble des compteurs agglomérés pour un message (O(1)).
func GetMessageReactionCounts(ctx context.Context, messageID int64) (map[string]int, error) {
	rawReactionMap, errRedis := redis.MessageReactionCounts.HGetAll(ctx, messageID).Result()

	if errRedis != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de récupérer les compteurs de réactions depuis le cache.",
			errRedis,
		)
	}

	reactionCounts := make(map[string]int)
	for emojiKey, countStr := range rawReactionMap {
		parsedCount, errParse := strconv.Atoi(countStr)
		if errParse != nil {
			numan_log.Warn(ctx).Err(errParse).Int64("message_id", messageID).Str("emoji", emojiKey).Msg("Valeur de compteur de réaction invalide dans Redis")
			continue
		}
		if parsedCount > 0 {
			reactionCounts[emojiKey] = parsedCount
		}
	}

	return reactionCounts, nil
}
