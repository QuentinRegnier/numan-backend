package object_cache_service

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	goredis "github.com/go-redis/redis/v8"
)

// ############################################################################
// # SERVICE : OBJECT CACHE (MÉDIAS LFU)
// ############################################################################

// SetMediaInObjectCache place les métadonnées de l'image en RAM (Write-Behind).
func SetMediaInObjectCache(ctx context.Context, mediaPayload media_models.MediaPayload) error {
	// Le TTL (ex: 24h) est défini dans le manager Redis et se réinitialise à chaque GET
	errRedis := redis.Media.SetObject(ctx, mediaPayload.ID, mediaPayload)
	if errRedis != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de placer le média dans l'Object Cache.", errRedis)
	}
	return nil
}

// GetMediaFromObjectCache récupère instantanément les métadonnées en O(1)
// avec un rejet immédiat si le média est soft-deleted.
func GetMediaFromObjectCache(ctx context.Context, mediaID int64) (media_models.MediaPayload, error) {
	var mediaPayload media_models.MediaPayload

	errRedis := redis.Media.GetObject(ctx, mediaID, &mediaPayload)
	if errRedis != nil {
		if errors.Is(errRedis, goredis.Nil) {
			return media_models.MediaPayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Média introuvable dans le cache.", errRedis)
		}
		return media_models.MediaPayload{}, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de récupérer le média depuis l'Object Cache.", errRedis)
	}

	// Rejet immédiat si le média est soft-deleted (-1)
	if mediaPayload.Visibility == variables.MediaVisibilitySoftDeleted {
		return media_models.MediaPayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Ce média a été supprimé.", nil)
	}

	return mediaPayload, nil
}

// DeleteMediaFromObjectCache purge les métadonnées de la RAM L1.
func DeleteMediaFromObjectCache(ctx context.Context, mediaID int64) error {
	errRedis := redis.Media.DeleteObject(ctx, mediaID)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de supprimer le média de l'Object Cache.",
			errRedis,
		)
	}
	return nil
}
