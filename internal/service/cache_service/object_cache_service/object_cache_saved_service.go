package object_cache_service

import (
	"context"
	"net/http"
	"strconv"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// ############################################################################
// # SERVICE : OBJECT CACHE (FAVORIS / SAVED POSTS ZSET)
// ############################################################################

// AddSavedToZSET ajoute un post dans le ZSET des favoris de l'utilisateur en L1.
func AddSavedToZSET(ctx context.Context, userID int64, postID int64, zsetScore float64) error {
	postIDString := strconv.FormatInt(postID, 10)

	errRedis := redis.Saved.ZAdd(ctx, userID, zsetScore, postIDString)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'ajouter le favori dans le cache.",
			errRedis,
		)
	}

	return nil
}

// RemoveSavedFromZSET retire un post des favoris de l'utilisateur en L1.
func RemoveSavedFromZSET(ctx context.Context, userID int64, postID int64) error {
	postIDString := strconv.FormatInt(postID, 10)

	errRedis := redis.Saved.ZRem(ctx, userID, postIDString)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de supprimer le favori du cache.",
			errRedis,
		)
	}

	return nil
}

// GetSavedPostIDs récupère la liste paginée des IDs de posts favoris depuis le ZSET L1.
func GetSavedPostIDs(ctx context.Context, userID int64, paginationOffset int64, paginationLimit int64) ([]int64, error) {
	idStringsList, errRedis := redis.Saved.ZRevRange(ctx, userID, paginationOffset, paginationOffset+paginationLimit-1)
	if errRedis != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de lire les identifiants de favoris depuis le cache.",
			errRedis,
		)
	}

	parsedPostIDsList := pkg.ParseInt64List(idStringsList)

	return parsedPostIDsList, nil
}
