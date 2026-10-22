package cache_service

import (
	"context"
	"net/http"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/sync_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// ############################################################################
// # SERVICE : CACHE DE PRÉSENCE EN LIGNE (O(1) L1)
// ############################################################################

// MarkUserOnline insère ou prolonge la présence de l'utilisateur dans le cache L1.
// Le TTL est strictement fixé à 90 secondes pour lisser les micro-coupures réseau
// (tolérance de 3 pings ratés de 30s).
func MarkUserOnline(ctx context.Context, userID int64) error {
	// 1. On stocke une simple string "1" pour minimiser l'empreinte RAM (O(1))
	errRedis := redis.Presence.SetPrimitive(ctx, userID, "1")
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de marquer l'utilisateur comme en ligne dans le cache.",
			errRedis,
		)
	}

	// 2. Application stricte du TTL de 90 secondes (Lissage de déconnexion)
	errExpire := redis.Expire(ctx, redis.Presence.Key(userID), 90*time.Second)
	if errExpire != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de prolonger la présence de l'utilisateur dans le cache.",
			errExpire,
		)
	}

	return nil
}

// IsUserOnline vérifie instantanément si un utilisateur est connecté sur n'importe quelle instance (O(1)).
func IsUserOnline(ctx context.Context, userID int64) bool {
	isOnline, errRedis := redis.Presence.Exists(ctx, userID)
	if errRedis != nil {
		numan_log.Warn(ctx).Err(errRedis).Int64("user_id", userID).Msg("Échec de la vérification de présence L1")
		return false // En cas de doute ou de panne cache, on considère hors-ligne
	}
	return isOnline
}

// AreUsersOnline récupère le statut de présence d'un lot d'utilisateurs en O(1) réseau.
// Utilise l'abstraction DDD de la Collection pour faire un MGET propre et ultra-rapide.
func AreUsersOnline(ctx context.Context, input sync_models.SyncPresenceInput) (map[int64]bool, error) {
	if err := pkg.ListLimitVerifDefault(input.UserIDs); err != nil {
		return nil, err
	}
	presenceResultsMap := make(map[int64]bool)

	if len(input.UserIDs) == 0 {
		return presenceResultsMap, nil
	}

	// Conversion du []int64 en []any exigé par la signature MGet de l'abstraction
	userIDsAsAny := make([]any, len(input.UserIDs))
	for index, id := range input.UserIDs {
		userIDsAsAny[index] = id
	}

	// Appel pur au repository/redis via la méthode abstraite MGet
	redisValues, errRedis := redis.Presence.MGet(ctx, userIDsAsAny...)
	if errRedis != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de récupérer les états de présence depuis le cache.",
			errRedis,
		)
	}

	for index, cachedValue := range redisValues {
		// val est nil si la clé n'existe pas (TTL de 90s expiré)
		// S'il y a quelque chose (le string "1" posé par MarkUserOnline), l'utilisateur est en ligne
		presenceResultsMap[input.UserIDs[index]] = cachedValue != nil
	}

	return presenceResultsMap, nil
}
