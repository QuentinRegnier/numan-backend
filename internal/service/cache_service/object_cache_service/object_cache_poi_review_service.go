package object_cache_service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	goredis "github.com/redis/go-redis/v9"
	"github.com/vmihailenco/msgpack/v5"
)

// ############################################################################
// # SERVICE : OBJECT & ZSET CACHE (AVIS & NOTES DE POI)
// ############################################################################

func userPOIReviewIndexKey(poiID, userID int64) string {
	return fmt.Sprintf("%d:%d", poiID, userID)
}

// GetPOIReviewFromObjectCache récupère un avis/note POI unique depuis le cache objet Redis L1.
func GetPOIReviewFromObjectCache(ctx context.Context, reviewID int64) (map_models.POIReviewPayload, error) {
	var review map_models.POIReviewPayload
	errRedis := redis.POIReviews.GetObject(ctx, reviewID, &review)
	if errRedis != nil {
		if errors.Is(errRedis, goredis.Nil) {
			return map_models.POIReviewPayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Avis POI introuvable dans le cache.", errRedis)
		}
		return map_models.POIReviewPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de récupérer l'avis POI depuis l'Object Cache.",
			errRedis,
		)
	}
	return review, nil
}

// GetPOIReviewsBatchFromObjectCache récupère un lot d'avis POI en un seul aller-retour MGET Redis L1.
func GetPOIReviewsBatchFromObjectCache(ctx context.Context, reviewIDs []int64) (map[int64]map_models.POIReviewPayload, []int64, error) {
	found := make(map[int64]map_models.POIReviewPayload, len(reviewIDs))
	if len(reviewIDs) == 0 {
		return found, nil, nil
	}

	res, err := redis.POIReviews.GetMany(ctx, reviewIDs)
	if err != nil {
		return nil, reviewIDs, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de récupérer le lot d'avis POI depuis l'Object Cache.",
			err,
		)
	}

	var missingIDs []int64
	for _, id := range reviewIDs {
		rawBytes, ok := res.Found[id]
		if !ok || len(rawBytes) == 0 {
			missingIDs = append(missingIDs, id)
			continue
		}
		var review map_models.POIReviewPayload
		if errUnmarshal := msgpack.Unmarshal(rawBytes, &review); errUnmarshal != nil {
			missingIDs = append(missingIDs, id)
			continue
		}
		found[id] = review
	}

	return found, missingIDs, nil
}

// SetPOIReviewInObjectCache enregistre ou met à jour un avis/note POI dans l'Object Cache Redis L1.
func SetPOIReviewInObjectCache(ctx context.Context, review map_models.POIReviewPayload) error {
	if err := redis.POIReviews.SetObject(ctx, review.ID, review); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de sauvegarder l'avis POI dans l'Object Cache.",
			err,
		)
	}
	return nil
}

// SetPOIReviewsBatchInObjectCache enregistre un lot d'avis POI dans l'Object Cache L1 en un seul pipeline.
func SetPOIReviewsBatchInObjectCache(ctx context.Context, reviews []map_models.POIReviewPayload) error {
	if len(reviews) == 0 {
		return nil
	}

	objMap := make(map[int64]any, len(reviews))
	for _, r := range reviews {
		objMap[r.ID] = r
	}

	if err := redis.POIReviews.SetMany(ctx, objMap); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de sauvegarder le lot d'avis POI dans l'Object Cache.",
			err,
		)
	}
	return nil
}

// DeletePOIReviewFromObjectCache supprime un avis/note POI de l'Object Cache L1.
func DeletePOIReviewFromObjectCache(ctx context.Context, reviewID int64) error {
	if err := redis.POIReviews.DeleteObject(ctx, reviewID); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de supprimer l'avis POI de l'Object Cache.",
			err,
		)
	}
	return nil
}

// GetUserPOIReviewID récupère l'identifiant de l'avis unique d'un utilisateur pour un POI donné depuis l'index L1.
func GetUserPOIReviewID(ctx context.Context, poiID, userID int64) (int64, error) {
	id, err := redis.POIReviewUserIndex.GetInt64(ctx, userPOIReviewIndexKey(poiID, userID))
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return 0, numan_error.NewNotFound(numan_error.CodeNotFound, "Index utilisateur/avis POI introuvable dans le cache.", err)
		}
		return 0, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de lire l'index d'avis POI utilisateur dans le cache.",
			err,
		)
	}
	return id, nil
}

// SetUserPOIReviewID enregistre la correspondance (poi_id, user_id) -> review_id dans l'index L1.
func SetUserPOIReviewID(ctx context.Context, poiID, userID, reviewID int64) error {
	if err := redis.POIReviewUserIndex.SetPrimitive(ctx, userPOIReviewIndexKey(poiID, userID), reviewID); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'enregistrer l'index d'avis POI utilisateur dans le cache.",
			err,
		)
	}
	return nil
}

// DeleteUserPOIReviewID supprime la correspondance (poi_id, user_id) -> review_id de l'index L1.
func DeleteUserPOIReviewID(ctx context.Context, poiID, userID int64) error {
	if err := redis.POIReviewUserIndex.DeletePrimitive(ctx, userPOIReviewIndexKey(poiID, userID)); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de supprimer l'index d'avis POI utilisateur du cache.",
			err,
		)
	}
	return nil
}

// AddTextReviewToZSET indexe un avis textuel (Content != "") dans le ZSET du POI, plafonné à MaxZsetPOIReview.
func AddTextReviewToZSET(ctx context.Context, poiID, reviewID, createdAt int64) error {
	member := strconv.FormatInt(reviewID, 10)
	if err := redis.POIReviewsByPOI.ZAddWithCap(ctx, poiID, float64(createdAt), member, variables.MaxZsetPOIReview); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'indexer l'avis textuel du POI dans le ZSET.",
			err,
		)
	}
	if errTTL := redis.POIReviewsByPOI.RefreshTTL(ctx, poiID); errTTL != nil {
		numan_log.Warn(ctx).Err(errTTL).Int64("poi_id", poiID).Msg("Impossible de rafraîchir le TTL du ZSET des avis POI")
	}
	return nil
}

// RemoveTextReviewFromZSET retire un avis du ZSET des avis textuels d'un POI.
func RemoveTextReviewFromZSET(ctx context.Context, poiID, reviewID int64) error {
	member := strconv.FormatInt(reviewID, 10)
	if err := redis.POIReviewsByPOI.ZRem(ctx, poiID, member); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de retirer l'avis du ZSET du POI.",
			err,
		)
	}
	return nil
}

// GetPOIReviewIDsFromZSET récupère les identifiants des avis textuels récents d'un POI depuis le ZSET L1.
func GetPOIReviewIDsFromZSET(ctx context.Context, poiID int64, offset, limit int) ([]int64, error) {
	if limit <= 0 {
		return []int64{}, nil
	}
	idStrings, err := redis.POIReviewsByPOI.ZRevRange(ctx, poiID, int64(offset), int64(offset+limit-1))
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de lire les identifiants d'avis POI depuis le ZSET.",
			err,
		)
	}
	return pkg.ParseInt64List(idStrings), nil
}

// PurgePOIReviewsFromL1 supprime du cache L1 tous les avis textuels indexés d'un POI soft-deleted
// (objets avis, index utilisateur et ZSET du POI).
func PurgePOIReviewsFromL1(ctx context.Context, poiID int64) {
	if poiID <= 0 {
		return
	}
	idStrings, errRedis := redis.POIReviewsByPOI.ZRevRange(ctx, poiID, 0, -1)
	if errRedis != nil {
		numan_log.Warn(ctx).Err(errRedis).Int64("poi_id", poiID).Msg("Impossible de lister les avis du ZSET POI pour purge L1")
	} else if len(idStrings) > 0 {
		reviewIDs := pkg.ParseInt64List(idStrings)
		if foundMap, _, errBatch := GetPOIReviewsBatchFromObjectCache(ctx, reviewIDs); errBatch == nil {
			for _, r := range foundMap {
				_ = DeleteUserPOIReviewID(ctx, poiID, r.UserID)
			}
		}
		if errDelBatch := redis.POIReviews.DeleteMany(ctx, reviewIDs); errDelBatch != nil {
			numan_log.Warn(ctx).Err(errDelBatch).Int64("poi_id", poiID).Msg("Impossible de purger les avis POI de l'Object Cache L1")
		}
	}

	if errDelZ := redis.POIReviewsByPOI.DeleteObject(ctx, poiID); errDelZ != nil {
		numan_log.Warn(ctx).Err(errDelZ).Int64("poi_id", poiID).Msg("Impossible de supprimer le ZSET des avis POI en L1")
	}
}
