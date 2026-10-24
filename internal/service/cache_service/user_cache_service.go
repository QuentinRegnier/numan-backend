package cache_service

import (
	"context"
	"net/http"
	"strconv"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : GESTION DE LA TIMELINE UTILISATEUR (ZSET)
// ############################################################################

// GetTopUserPostIDs récupère la timeline, gère les profils vides, et prévient
// de manière déterministe les cache misses vers MongoDB/Postgres.
func GetTopUserPostIDs(ctx context.Context, userID int64, offset int64, limit int64) ([]int64, error) {

	// ── ÉTAPE 1 : VÉRIFICATION D'EXISTENCE (ÉVITE LE CACHE MISS BDD) ────────

	isZSetPresent, errExists := redis.UserTimeline.Exists(ctx, userID)
	if errExists != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Erreur lors de la vérification de l'existence de la timeline utilisateur",
			errExists,
		)
	}

	if !isZSetPresent {
		// Le retour d'une erreur déclenchera le fallback (L2 -> L3) par la couche appelante
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Timeline utilisateur non présente dans le cache",
			nil,
		)
	}

	// ── ÉTAPE 2 : EXTRACTION PAGINÉE DU ZSET (O(log(N)+M)) ──────────────────

	idStringsList, errRedis := redis.UserTimeline.ZRevRange(ctx, userID, offset, offset+limit-1)
	if errRedis != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Échec de lecture de la timeline utilisateur",
			errRedis,
		)
	}
	var validStrings []string
	for _, idString := range idStringsList {
		if idString != variables.UserTimelineEmptyMarker {
			validStrings = append(validStrings, idString)
		}
	}
	parsedPostIDs := pkg.ParseInt64List(validStrings)

	return parsedPostIDs, nil
}

// MarkUserTimelineEmpty injecte un marqueur fictif pour sceller le Cache Hit
// et indiquer qu'un utilisateur n'a réellement aucune publication à son actif.
func MarkUserTimelineEmpty(ctx context.Context, userID int64) error {
	errRedis := redis.UserTimeline.ZAdd(ctx, userID, 0, variables.UserTimelineEmptyMarker)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de marquer la timeline comme vide",
			errRedis,
		)
	}

	if errTTL := redis.UserTimeline.RefreshTTL(ctx, userID); errTTL != nil {
		numan_log.Warn(ctx).Err(errTTL).Int64("user_id", userID).Msg("Impossible de rafraîchir le TTL de la timeline utilisateur")
	}
	return nil
}

// PurgeUserTimeline détruit totalement le ZSET associé à l'utilisateur.
func PurgeUserTimeline(ctx context.Context, userID int64) error {
	// DeleteObject encapsule le DEL physique de la clé Redis (Pur DDD)
	errRedis := redis.UserTimeline.DeleteObject(ctx, userID)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Erreur lors de la purge de la timeline utilisateur",
			errRedis,
		)
	}
	return nil
}

// AddPostToUserProfile insère un nouveau post au sommet de la timeline et retire
// automatiquement le marqueur "profil vide" s'il existait.
func AddPostToUserProfile(ctx context.Context, userID int64, postID int64, timestampScore float64) error {

	// Nettoyage préventif
	if errRem := redis.UserTimeline.ZRem(ctx, userID, variables.UserTimelineEmptyMarker); errRem != nil {
		numan_log.Warn(ctx).Err(errRem).Int64("user_id", userID).Msg("Impossible de retirer le marqueur de timeline vide")
	}

	postIDString := strconv.FormatInt(postID, 10)
	errRedis := redis.UserTimeline.ZAdd(ctx, userID, timestampScore, postIDString)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Échec de l'ajout du post dans la timeline utilisateur",
			errRedis,
		)
	}

	return nil
}

// RemovePostFromUserProfile expulse l'ID d'une publication de la timeline (ex: Soft Delete).
func RemovePostFromUserProfile(ctx context.Context, userID int64, postID int64) error {
	postIDString := strconv.FormatInt(postID, 10)
	errRedis := redis.UserTimeline.ZRem(ctx, userID, postIDString)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Échec du retrait du post dans la timeline utilisateur",
			errRedis,
		)
	}
	return nil
}
