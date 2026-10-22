package object_cache_service

import (
	"context"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// ############################################################################
// # SERVICE : OBJECT CACHE (PUBLICATIONS / POSTS LFU)
// ############################################################################

// GetPostFromObjectCache récupère un post depuis le cache L1.
func GetPostFromObjectCache(ctx context.Context, postID int64) (post_models.PostPayload, error) {
	var postPayload post_models.PostPayload

	errRedis := redis.Posts.GetObject(ctx, postID, &postPayload)
	if errRedis != nil {
		return post_models.PostPayload{}, errRedis
	}

	return postPayload, nil
}

// SetPostInObjectCache enregistre ou met à jour un post dans le cache L1.
func SetPostInObjectCache(ctx context.Context, postPayload post_models.PostPayload) error {
	errRedis := redis.Posts.SetObject(ctx, postPayload.ID, postPayload)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de sauvegarder le post dans l'Object Cache.",
			errRedis,
		)
	}
	return nil
}

// DeletePostFromObjectCache purge instantanément un post du cache L1.
func DeletePostFromObjectCache(ctx context.Context, postID int64) error {
	errRedis := redis.Posts.DeleteObject(ctx, postID)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de supprimer le post de l'Object Cache.",
			errRedis,
		)
	}
	return nil
}

// IsPostInObjectCache vérifie silencieusement et rapidement si un post est en RAM (O(1)).
func IsPostInObjectCache(ctx context.Context, postID int64) bool {
	isExists, errRedis := redis.Posts.Exists(ctx, postID)
	if errRedis != nil {
		return false
	}
	return isExists
}
