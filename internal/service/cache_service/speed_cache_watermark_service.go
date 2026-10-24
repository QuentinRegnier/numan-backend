package cache_service

import (
	"context"
	"net/http"
	"strconv"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// ############################################################################
// # SERVICE : SPEED CACHE (CURSEURS DE LECTURE / WATERMARKS)
// ############################################################################

// SetWatermarkInSpeedCache met à jour le curseur de lecture d'un utilisateur
// dans une conversation (O(1)).
func SetWatermarkInSpeedCache(ctx context.Context, conversationID int64, userID int64, lastReadMessageID int64) error {

	// Le HSET crée la clé si elle n'existe pas, ou met à jour le champ (userID) avec la nouvelle valeur.
	errRedis := redis.ConvWatermarks.HSet(ctx, conversationID, strconv.FormatInt(userID, 10), lastReadMessageID)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de mettre à jour le watermark dans le Speed Cache L1",
			errRedis,
		)
	}

	// On prolonge la durée de vie de cette structure en RAM à chaque activité
	if errTTL := redis.ConvWatermarks.RefreshTTL(ctx, conversationID); errTTL != nil {
		numan_log.Warn(ctx).Err(errTTL).Entity("conversation", conversationID).Msg("Impossible de rafraîchir le TTL des watermarks de conversation")
	}

	return nil
}

// GetWatermarksFromSpeedCache récupère tous les curseurs de lecture d'une conversation d'un seul coup (O(1)).
func GetWatermarksFromSpeedCache(ctx context.Context, conversationID int64) (map[int64]int64, error) {

	rawWatermarksMap, errRedis := redis.ConvWatermarks.HGetAll(ctx, conversationID).Result()
	if errRedis != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Erreur L1 lors de la récupération des watermarks de la conversation",
			errRedis,
		)
	}

	parsedWatermarksMap := make(map[int64]int64)
	for userIDString, messageIDString := range rawWatermarksMap {
		parsedUserID, errParseUser := pkg.ParseInt64Strict(userIDString)
		parsedMessageID, errParseMsg := pkg.ParseInt64Strict(messageIDString)

		if errParseUser == nil && errParseMsg == nil {
			parsedWatermarksMap[parsedUserID] = parsedMessageID
		}
	}

	// Si des données sont présentes, on maintient le cache en vie (LFU/LRU behavior)
	if len(parsedWatermarksMap) > 0 {
		if errTTL := redis.ConvWatermarks.RefreshTTL(ctx, conversationID); errTTL != nil {
			numan_log.Warn(ctx).Err(errTTL).Entity("conversation", conversationID).Msg("Impossible de rafraîchir le TTL des watermarks de conversation")
		}
	}

	return parsedWatermarksMap, nil
}
