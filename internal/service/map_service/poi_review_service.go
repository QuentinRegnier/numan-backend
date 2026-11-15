package map_service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/QuentinRegnier/numan-backend/internal/worker"
)

// ############################################################################
// # SERVICE : AVIS & NOTES DES POINTS D'INTÉRÊT (UNIFIÉ + COMPTEURS BATCH)
// ############################################################################

// CreatePOIReviewService crée ou met à jour l'avis/note unique d'un utilisateur pour un POI.
// Une note sans texte est stockée de manière unifiée avec Content = "".
// Ne déclenche JAMAIS de mise à jour complète de POIPayload en base :
// - Met à jour instantanément le POIPayload en RAM (L1)
// - Enregistre le delta (rating_sum, review_count) dans Interaction Worker pour flush batché
// - Envoie le POIReviewPayload en Write-Behind vers PostgreSQL L3
func CreatePOIReviewService(
	ctx context.Context,
	callerID int64,
	input map_models.CreatePOIReviewInput,
) (map_models.CreatePOIReviewOutput, error) {
	if callerID <= 0 {
		return map_models.CreatePOIReviewOutput{}, numan_error.NewUnauthorized(
			numan_error.CodeUnauthorized,
			"Authentification requise pour noter un point d'intérêt.",
			nil,
		)
	}
	if err := input.Validate(); err != nil {
		return map_models.CreatePOIReviewOutput{}, err
	}

	content := strings.TrimSpace(input.Content)

	// 1. Récupération du POI parent (L1 -> L3)
	poi, errPOI := getPOIWithFallback(ctx, input.POIID)
	if errPOI != nil {
		return map_models.CreatePOIReviewOutput{}, errPOI
	}

	// 2. Vérification d'existence d'un avis/note précédent (contrainte 1 avis par utilisateur et par POI)
	existing, exists, errFind := findUserPOIReview(ctx, input.POIID, callerID)
	if errFind != nil {
		return map_models.CreatePOIReviewOutput{}, errFind
	}

	now := domain.NowMillis()
	var (
		review      map_models.POIReviewPayload
		isUpdate    bool
		oldHadText  bool
		ratingDelta int64
		countDelta  int
	)

	if exists {
		isUpdate = true
		oldHadText = strings.TrimSpace(existing.Content) != ""
		ratingDelta = int64(input.Rating) - int64(existing.Rating)
		countDelta = 0

		review = existing
		review.Rating = input.Rating
		review.Content = content
		review.Visibility = variables.POIReviewVisibilityPublic
		review.UpdatedAt = now
	} else {
		reviewID := pkg.GenerateID()
		if reviewID == 0 {
			reviewID = now
		}
		ratingDelta = int64(input.Rating)
		countDelta = 1

		review = map_models.POIReviewPayload{
			ID:         reviewID,
			POIID:      input.POIID,
			UserID:     callerID,
			Rating:     input.Rating,
			Content:    content,
			Visibility: variables.POIReviewVisibilityPublic,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
	}

	if errVal := review.Validate(); errVal != nil {
		return map_models.CreatePOIReviewOutput{}, errVal
	}

	// 3. Mise à jour synchrone L1 (Object Cache de l'avis + Index utilisateur + ZSET textuel)
	if errSet := object_cache_service.SetPOIReviewInObjectCache(ctx, review); errSet != nil {
		return map_models.CreatePOIReviewOutput{}, errSet
	}
	if errIdx := object_cache_service.SetUserPOIReviewID(ctx, input.POIID, callerID, review.ID); errIdx != nil {
		return map_models.CreatePOIReviewOutput{}, errIdx
	}

	if review.Content != "" {
		if errZ := object_cache_service.AddTextReviewToZSET(ctx, input.POIID, review.ID, review.CreatedAt); errZ != nil {
			numan_log.Warn(ctx).Err(errZ).Int64("poi_id", input.POIID).Int64("review_id", review.ID).Msg("Échec indexation avis textuel dans ZSET L1")
		}
	} else if isUpdate && oldHadText {
		if errZ := object_cache_service.RemoveTextReviewFromZSET(ctx, input.POIID, review.ID); errZ != nil {
			numan_log.Warn(ctx).Err(errZ).Int64("poi_id", input.POIID).Int64("review_id", review.ID).Msg("Échec suppression avis devenu note pure du ZSET L1")
		}
	}

	// 4. Recalcul instantané en RAM du POIPayload (L1 uniquement — aucune écriture complète POI en BDD)
	applyPOICounterDelta(&poi, ratingDelta, countDelta, now)
	if errPOICache := object_cache_service.SetPOIInObjectCache(ctx, poi); errPOICache != nil {
		return map_models.CreatePOIReviewOutput{}, errPOICache
	}

	// 5. Persistance asynchrone Write-Behind de l'avis via ActionCreate (routé vers func_upsert_poi_review en L3)
	if errQ := redis.EnqueueDB(ctx, review.ID, input.POIID, redis.EntityPOIReview, redis.ActionCreate, review, redis.TargetPostgres); errQ != nil {
		numan_log.Error(ctx).Err(errQ).Int64("poi_id", input.POIID).Int64("review_id", review.ID).Msg("Échec EnqueueDB lors de la création/mise à jour d'un avis POI")
		return map_models.CreatePOIReviewOutput{}, errQ
	}

	// 6. Enregistrement du delta de compteurs dans l'Interaction Worker (flush batché unique par POI)
	if ratingDelta != 0 || countDelta != 0 {
		worker.RegisterPOIReview(input.POIID, ratingDelta, countDelta)
	}

	numan_log.Info(ctx).
		Int64(numan_log.LogKeyUserID, callerID).
		Int64("poi_id", input.POIID).
		Int64("review_id", review.ID).
		Bool("is_update", isUpdate).
		Msg("Avis/note POI enregistré avec succès")

	return map_models.CreatePOIReviewOutput{
		Review:          hydratePOIReviewView(ctx, callerID, review),
		POIRating:       poi.Rating,
		POICommentCount: poi.CommentCount,
		IsUpdate:        isUpdate,
	}, nil
}

// GetPOIReviewsService récupère les avis textuels paginés d'un POI (L1 ZSET -> L3 PostgreSQL).
// RÈGLE CRITIQUE : Seuls les avis contenant du texte (Content != "") sont retournés ;
// les notes pures sans texte sont strictement ignorées à l'affichage.
func GetPOIReviewsService(
	ctx context.Context,
	callerID int64,
	input map_models.GetPOIReviewsInput,
) (map_models.GetPOIReviewsOutput, error) {
	if err := input.Validate(); err != nil {
		return map_models.GetPOIReviewsOutput{}, err
	}

	limit := input.Limit
	if limit == 0 {
		limit = variables.DefaultPOIReviewLimit
	} else if limit > variables.MaxPOIReviewLimit {
		limit = variables.MaxPOIReviewLimit
	}

	// Vérification que le POI existe (L1 -> L3)
	if _, errPOI := getPOIWithFallback(ctx, input.POIID); errPOI != nil {
		return map_models.GetPOIReviewsOutput{}, errPOI
	}

	var reviews []map_models.POIReviewPayload

	// 1. Fast Path L1 (ZSET des avis textuels uniquement)
	if input.Offset < variables.MaxZsetPOIReview {
		zsetIDs, errZ := object_cache_service.GetPOIReviewIDsFromZSET(ctx, input.POIID, input.Offset, limit)
		if errZ == nil && len(zsetIDs) > 0 {
			foundMap, missingIDs, errBatch := object_cache_service.GetPOIReviewsBatchFromObjectCache(ctx, zsetIDs)
			if errBatch != nil {
				foundMap = make(map[int64]map_models.POIReviewPayload)
				missingIDs = zsetIDs
			}

			if len(missingIDs) > 0 && infradb.PostgresDB != nil {
				l3Missing, errL3 := postgres.FuncGetPOIReviewsByIDs(ctx, missingIDs)
				if errL3 == nil && len(l3Missing) > 0 {
					_ = object_cache_service.SetPOIReviewsBatchInObjectCache(ctx, l3Missing)
					for _, r := range l3Missing {
						foundMap[r.ID] = r
					}
				}
			}

			for _, id := range zsetIDs {
				if r, ok := foundMap[id]; ok && r.Visibility >= variables.POIReviewVisibilityPublic && strings.TrimSpace(r.Content) != "" {
					reviews = append(reviews, r)
				}
			}

			views := make([]map_models.POIReviewView, 0, len(reviews))
			for _, r := range reviews {
				views = append(views, hydratePOIReviewView(ctx, callerID, r))
			}
			return map_models.GetPOIReviewsOutput{Reviews: views}, nil
		}
	}

	// 2. Fallback PostgreSQL L3 (lorsque le ZSET L1 est vide ou au-delà du plafond)
	if infradb.PostgresDB != nil {
		l3Reviews, errL3 := postgres.FuncGetPOIReviewsPaginated(ctx, input.POIID, limit, input.Offset)
		if errL3 != nil {
			return map_models.GetPOIReviewsOutput{}, errL3
		}
		if len(l3Reviews) > 0 {
			liveReviews := make([]map_models.POIReviewPayload, 0, len(l3Reviews))
			for _, r := range l3Reviews {
				if r.Visibility < variables.POIReviewVisibilityPublic {
					continue
				}
				liveReviews = append(liveReviews, r)
				if strings.TrimSpace(r.Content) != "" {
					if input.Offset < variables.MaxZsetPOIReview {
						_ = object_cache_service.AddTextReviewToZSET(ctx, input.POIID, r.ID, r.CreatedAt)
					}
					reviews = append(reviews, r)
				}
			}
			if len(liveReviews) > 0 {
				_ = object_cache_service.SetPOIReviewsBatchInObjectCache(ctx, liveReviews)
			}
		}
	}

	views := make([]map_models.POIReviewView, 0, len(reviews))
	for _, r := range reviews {
		views = append(views, hydratePOIReviewView(ctx, callerID, r))
	}
	return map_models.GetPOIReviewsOutput{Reviews: views}, nil
}

// DeletePOIReviewService supprime l'avis/note de l'utilisateur appelant sur un POI,
// purge L1 instantanément, recalcule la note moyenne et le compteur en RAM,
// et programme la suppression asynchrone + décrémentation batchée vers PostgreSQL L3.
func DeletePOIReviewService(
	ctx context.Context,
	callerID int64,
	input map_models.DeletePOIReviewInput,
) (map_models.DeletePOIReviewOutput, error) {
	if callerID <= 0 {
		return map_models.DeletePOIReviewOutput{}, numan_error.NewUnauthorized(
			numan_error.CodeUnauthorized,
			"Authentification requise pour supprimer un avis.",
			nil,
		)
	}
	if err := input.Validate(); err != nil {
		return map_models.DeletePOIReviewOutput{}, err
	}

	poi, errPOI := getPOIWithFallback(ctx, input.POIID)
	if errPOI != nil {
		return map_models.DeletePOIReviewOutput{}, errPOI
	}

	existing, exists, errFind := findUserPOIReview(ctx, input.POIID, callerID)
	if errFind != nil {
		return map_models.DeletePOIReviewOutput{}, errFind
	}
	if !exists {
		return map_models.DeletePOIReviewOutput{}, numan_error.NewNotFound(
			numan_error.CodeNotFound,
			"Aucun avis trouvé pour cet utilisateur sur ce point d'intérêt.",
			nil,
		)
	}

	ratingDelta := -int64(existing.Rating)
	countDelta := -1
	now := domain.NowMillis()
	existing.Visibility = variables.POIReviewVisibilitySoftDeleted
	existing.UpdatedAt = now

	// 1. Purge synchrone L1
	if errDel := object_cache_service.DeletePOIReviewFromObjectCache(ctx, existing.ID); errDel != nil {
		return map_models.DeletePOIReviewOutput{}, errDel
	}
	if errIdx := object_cache_service.DeleteUserPOIReviewID(ctx, input.POIID, callerID); errIdx != nil {
		return map_models.DeletePOIReviewOutput{}, errIdx
	}
	if errZ := object_cache_service.RemoveTextReviewFromZSET(ctx, input.POIID, existing.ID); errZ != nil {
		numan_log.Warn(ctx).Err(errZ).Int64("poi_id", input.POIID).Int64("review_id", existing.ID).Msg("Échec suppression avis du ZSET L1")
	}

	// 2. Recalcul instantané en RAM du POIPayload
	applyPOICounterDelta(&poi, ratingDelta, countDelta, now)
	if errPOICache := object_cache_service.SetPOIInObjectCache(ctx, poi); errPOICache != nil {
		return map_models.DeletePOIReviewOutput{}, errPOICache
	}

	// 3. Persistance Write-Behind (Soft Delete visibilité = -1 pour conservation légale + batching du compteur)
	delPayload := map[string]any{
		"poi_id":     input.POIID,
		"user_id":    callerID,
		"visibility": variables.POIReviewVisibilitySoftDeleted,
		"updated_at": now,
	}
	if errQ := redis.EnqueueDB(ctx, existing.ID, input.POIID, redis.EntityPOIReview, redis.ActionDelete, delPayload, redis.TargetPostgres); errQ != nil {
		numan_log.Error(ctx).Err(errQ).Int64("poi_id", input.POIID).Int64("review_id", existing.ID).Msg("Échec EnqueueDB lors du Soft Delete de l'avis POI")
		return map_models.DeletePOIReviewOutput{}, errQ
	}

	worker.RegisterPOIReview(input.POIID, ratingDelta, countDelta)

	numan_log.Info(ctx).
		Int64(numan_log.LogKeyUserID, callerID).
		Int64("poi_id", input.POIID).
		Int64("review_id", existing.ID).
		Action(numan_log.ActionSoftDelete).
		Msg("Avis/note POI supprimé (Soft Delete) avec succès")

	return map_models.DeletePOIReviewOutput{
		POIID:           input.POIID,
		Deleted:         true,
		POIRating:       poi.Rating,
		POICommentCount: poi.CommentCount,
	}, nil
}

// ############################################################################
// # HELPERS INTERNES
// ############################################################################

func isNotFoundError(err error) bool {
	var appErr *numan_error.AppError
	return errors.As(err, &appErr) && appErr.HTTPStatus == http.StatusNotFound
}

func getPOIWithFallback(ctx context.Context, poiID int64) (map_models.POIPayload, error) {
	poi, errL1 := object_cache_service.GetPOIFromObjectCache(ctx, poiID)
	if errL1 == nil {
		if poi.Visibility < variables.POIVisibilityPublic {
			_ = object_cache_service.DeletePOIsFromObjectCache(ctx, []int64{poiID})
			return map_models.POIPayload{}, numan_error.NewNotFound(
				numan_error.CodeNotFound,
				fmt.Sprintf("Le point d'intérêt %d est introuvable.", poiID),
				nil,
			)
		}
		return poi, nil
	}

	if infradb.PostgresDB == nil {
		if isNotFoundError(errL1) {
			return map_models.POIPayload{}, numan_error.NewNotFound(
				numan_error.CodeNotFound,
				fmt.Sprintf("Le point d'intérêt %d est introuvable.", poiID),
				nil,
			)
		}
		return map_models.POIPayload{}, errL1
	}

	l3POIs, errL3 := postgres.FuncGetPOIsByIDs(ctx, []int64{poiID})
	if errL3 != nil {
		return map_models.POIPayload{}, errL3
	}
	if len(l3POIs) == 0 || l3POIs[0].Visibility < variables.POIVisibilityPublic {
		return map_models.POIPayload{}, numan_error.NewNotFound(
			numan_error.CodeNotFound,
			fmt.Sprintf("Le point d'intérêt %d est introuvable.", poiID),
			nil,
		)
	}

	poi = l3POIs[0]
	_ = object_cache_service.SetPOIInObjectCache(ctx, poi)
	return poi, nil
}

func findUserPOIReview(ctx context.Context, poiID, userID int64) (map_models.POIReviewPayload, bool, error) {
	reviewID, errIdx := object_cache_service.GetUserPOIReviewID(ctx, poiID, userID)
	if errIdx == nil && reviewID > 0 {
		review, errObj := object_cache_service.GetPOIReviewFromObjectCache(ctx, reviewID)
		if errObj == nil {
			if review.Visibility < variables.POIReviewVisibilityPublic {
				_ = object_cache_service.DeletePOIReviewFromObjectCache(ctx, review.ID)
				_ = object_cache_service.DeleteUserPOIReviewID(ctx, poiID, userID)
			} else {
				return review, true, nil
			}
		} else if !isNotFoundError(errObj) {
			return map_models.POIReviewPayload{}, false, errObj
		}
	} else if errIdx != nil && !isNotFoundError(errIdx) {
		return map_models.POIReviewPayload{}, false, errIdx
	}

	if infradb.PostgresDB == nil {
		return map_models.POIReviewPayload{}, false, nil
	}

	existing, found, errL3 := postgres.FuncGetPOIReviewByUser(ctx, poiID, userID)
	if errL3 != nil {
		return map_models.POIReviewPayload{}, false, errL3
	}
	if !found || existing.Visibility < variables.POIReviewVisibilityPublic {
		return map_models.POIReviewPayload{}, false, nil
	}

	_ = object_cache_service.SetPOIReviewInObjectCache(ctx, existing)
	_ = object_cache_service.SetUserPOIReviewID(ctx, poiID, userID, existing.ID)
	return existing, true, nil
}

// applyPOICounterDelta recalcule la moyenne pondérée et le nombre total d'avis en RAM de façon déterministe.
func applyPOICounterDelta(poi *map_models.POIPayload, ratingDelta int64, countDelta int, updatedAt int64) {
	newCount := poi.CommentCount + countDelta
	if newCount <= 0 {
		poi.CommentCount = 0
		poi.Rating = 0.0
		poi.UpdatedAt = updatedAt
		return
	}

	totalSum := (poi.Rating * float64(poi.CommentCount)) + float64(ratingDelta)
	newAvg := totalSum / float64(newCount)
	if newAvg < 0.0 {
		newAvg = 0.0
	} else if newAvg > 5.0 {
		newAvg = 5.0
	}

	poi.CommentCount = newCount
	poi.Rating = newAvg
	poi.UpdatedAt = updatedAt
}

func hydratePOIReviewView(ctx context.Context, callerID int64, r map_models.POIReviewPayload) map_models.POIReviewView {
	var authorUsername string
	var avatarView media_models.MediaView

	if authorLite, errLite := cache_service.GetUserLite(ctx, r.UserID); errLite == nil {
		authorUsername = authorLite.Username
		if authorLite.ProfilePictureID > 0 {
			if view, errMedia := media_service.GenerateMediaViewCascade(ctx, authorLite.ProfilePictureID, authorLite.ID, 0, callerID); errMedia == nil {
				avatarView = view
			}
		}
	}

	return r.ToView(authorUsername, avatarView)
}
