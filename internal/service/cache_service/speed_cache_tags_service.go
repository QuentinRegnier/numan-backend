package cache_service

import (
	"context"
	"net/http"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : SPEED CACHE (HASHTAGS)
// ############################################################################

// SearchTagsByPrefix recherche des hashtags en O(log N) RAM pour l'autocomplétion avec pagination.
func SearchTagsByPrefix(ctx context.Context, searchPrefix string, searchOffset, searchLimit int64) ([]string, error) {

	// 1. Recherche ultra-rapide dans l'index lexicographique global
	// On passe désormais searchOffset au wrapper du repository Redis
	lexicographicResults, errRedis := redis.TagsLex.ZRangeByLex(ctx, variables.LexicographicGlobalKey, strings.ToLower(searchPrefix), searchOffset, searchLimit)
	if errRedis != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de rechercher les hashtags dans le cache.",
			errRedis,
		)
	}

	if len(lexicographicResults) == 0 {
		return []string{}, nil
	}

	// 2. Formatage du retour
	// Pour les tags, la valeur stockée (slug) se suffit à elle-même, pas besoin de Split ou d'ID.
	var extractedTagsList []string
	for _, resultString := range lexicographicResults {
		extractedTagsList = append(extractedTagsList, resultString)
	}

	return extractedTagsList, nil
}

// StoreTagInSpeedCache ajoute un nouveau hashtag dans l'index d'autocomplétion.
// À appeler depuis le hashtag_canon worker lorsqu'un nouveau tag est officialisé.
func StoreTagInSpeedCache(ctx context.Context, newTag string) error {
	errRedis := redis.TagsLex.ZAdd(ctx, variables.LexicographicGlobalKey, 0, strings.ToLower(newTag))
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'insérer le tag dans l'index du cache.",
			errRedis,
		)
	}
	return nil
}

// IsTagTrending vérifie en O(1) dans la RAM si un tag fait partie du Top Hype mondial.
func IsTagTrending(ctx context.Context, targetTag string) bool {
	// Le Leaderboard stocke les tags avec leur score de hype.
	// Si le ZScore renvoie une valeur (err == nil), c'est que le tag est dans le classement.
	leaderboardKey := redis.HashtagLeaderboard.Key("global")
	_, errRedis := redis.HashtagLeaderboard.Client.ZScore(ctx, leaderboardKey, targetTag).Result()

	return errRedis == nil
}
