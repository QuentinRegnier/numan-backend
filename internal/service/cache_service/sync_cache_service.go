package cache_service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// ############################################################################
// # SERVICE : ÉCRITURES DANS LE LEDGER DE SYNCHRONISATION (TRIGGERS)
// ############################################################################

// RecordConversationMutation enregistre une modification globale sur la conversation
// dans le Ledger de CHAQUE participant en un seul aller-retour réseau (O(1) Pipeline Redis).
// Utilisé pour les changements de nom, avatars, permissions, mute, kick...
func RecordConversationMutation(ctx context.Context, conversationID int64, participantIDsList []int64) error {

	currentTimestampMs := float64(domain.NowMillis())
	conversationIDString := strconv.FormatInt(conversationID, 10)

	// DÉLÉGATION DDD ABSOLUE : Le Cache Service ne parle qu'à l'abstraction Collection
	errRedis := redis.UserSyncLedger.ZAddMultiple(ctx, participantIDsList, currentTimestampMs, conversationIDString)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Échec de la mutation du Ledger Conversation",
			errRedis,
		)
	}

	return nil
}

// RecordMessageMutation enregistre une modification granulaire sur un message
// (Édition, Suppression, Réaction) ET signale la conversation entière comme mutée aux participants.
func RecordMessageMutation(ctx context.Context, conversationID int64, messageID int64, participantIDsList []int64) error {

	currentTimestampMs := float64(domain.NowMillis())
	messageIDString := strconv.FormatInt(messageID, 10)

	// 1. Inscription du message dans le Ledger (ZSET unique par conversation)
	errRedis := redis.ConvMessageLedger.ZAdd(ctx, conversationID, currentTimestampMs, messageIDString)
	if errRedis == nil {
		_ = redis.ConvMessageLedger.RefreshTTL(ctx, conversationID)
	} else {
		numan_log.Warn(ctx).Err(errRedis).Int64("message_id", messageID).Msg("Échec de l'insertion dans le ConvMessageLedger")
	}

	// 2. On "allume le gyrophare" sur la conversation parente pour tous les participants
	return RecordConversationMutation(ctx, conversationID, participantIDsList)
}

// ############################################################################
// # SERVICE : LECTURES DEPUIS LE LEDGER (PULL SYNCHRONIZATION)
// ############################################################################

// GetModifiedConversationIDs retourne la liste des IDs de conversations ayant muté depuis un timestamp.
func GetModifiedConversationIDs(ctx context.Context, userID int64, sinceTimestampMs int64) ([]int64, error) {

	// Syntaxe Redis : "(" indique "strictement supérieur"
	minScoreThreshold := fmt.Sprintf("(%d", sinceTimestampMs)
	maxScoreThreshold := "+inf"

	// DÉLÉGATION DDD : O(log N) abstrait et sécurisé (limite arbitraire fixée à 1000 pour protéger la RAM)
	idStringsList, errRedis := redis.UserSyncLedger.ZRangeByScore(ctx, userID, minScoreThreshold, maxScoreThreshold, 1000)

	if errRedis != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Erreur lors de la lecture du UserSyncLedger",
			errRedis,
		)
	}

	parsedConversationIDs := pkg.ParseInt64List(idStringsList)

	return parsedConversationIDs, nil
}

// GetModifiedMessageIDs retourne la liste des IDs de messages ayant muté dans une conversation spécifique.
func GetModifiedMessageIDs(ctx context.Context, conversationID int64, sinceTimestampMs int64) ([]int64, error) {

	minScoreThreshold := fmt.Sprintf("(%d", sinceTimestampMs)
	maxScoreThreshold := "+inf"

	// DÉLÉGATION DDD
	idStringsList, errRedis := redis.ConvMessageLedger.ZRangeByScore(ctx, conversationID, minScoreThreshold, maxScoreThreshold, 1000)

	if errRedis != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Erreur lors de la lecture du ConvMessageLedger",
			errRedis,
		)
	}

	parsedMessageIDs := pkg.ParseInt64List(idStringsList)

	return parsedMessageIDs, nil
}
