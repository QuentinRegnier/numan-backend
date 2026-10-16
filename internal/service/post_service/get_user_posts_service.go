package post_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
)

// ############################################################################
// # SERVICE : RÉCUPÉRATION DE LA TIMELINE D'UN UTILISATEUR
// ############################################################################

// GetUserPosts retourne la timeline complète d'un utilisateur.
func GetUserPosts(ctx context.Context, callerID int64, input post_models.GetUserPostsInput) ([]post_models.GetPostOutput, error) {
	var errOffset, errLimit *numan_error.AppError
	input.Offset, errOffset, input.Limit, errLimit = pkg.BatchVerif(input.Offset, input.Limit)
	if errOffset != nil || errLimit != nil {
		return []post_models.GetPostOutput{}, numan_error.Combine(errOffset, errLimit)
	}

	// ── ÉTAPE 1 : TENTATIVE L1 (ZSET USER CACHE) ────────────────────────────
	var postIDsList []int64
	var errCache error

	// Sauf si contournement forcé par l'utilisateur (Force=true)
	if !input.Force {
		postIDsList, errCache = cache_service.GetTopUserPostIDs(ctx, input.TargetUserID, input.Offset, input.Limit)
	}

	// Si errCache == nil et sans force, on a tapé le cache (Même si la liste est vide grâce au marqueur).
	if errCache == nil && !input.Force {
		if len(postIDsList) == 0 {
			// Remplacement du "nil" : On lève une erreur NotFound explicite
			return []post_models.GetPostOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Aucune publication trouvée pour cet utilisateur.", nil)
		}
		// Capture propre de l'erreur propagée par GetPosts au lieu de forcer "nil"
		outputs, err := GetPosts(ctx, callerID, post_models.GetPostInput{PostIDs: postIDsList})
		return outputs, err
	}

	// ── ÉTAPE 2 : FALLBACK ABSOLU L3 (POSTGRESQL) - CONTOURNEMENT MONGO ─────
	postsFromPostgres, errPg := postgres.FuncLoadUserPostsKeyset(ctx, input.TargetUserID, input.BeforeCreatedAt, input.BeforeID, input.Limit)
	if errPg != nil {
		numan_log.Error(ctx).Err(errPg).Int64("target_user_id", input.TargetUserID).Msg("Échec L3 lors de la récupération de la timeline utilisateur")
		return []post_models.GetPostOutput{}, numan_error.NewInternal(errPg)
	}

	// ── ÉTAPE 3 : HYDRATATION EN TEMPS RÉEL (PROTECTION & RECONSTRUCTION) ───
	if len(postsFromPostgres) == 0 {
		// Protection anti-fantôme même en mode force : on certifie le vide à Redis.
		_ = cache_service.MarkUserTimelineEmpty(ctx, input.TargetUserID)
		// Remplacement du "nil" : On lève une erreur NotFound explicite
		return []post_models.GetPostOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Aucune publication trouvée pour cet utilisateur.", nil)
	}

	// PURGE AVANT BATCH : On rase le ZSET pour écraser proprement un éventuel "-1" ou une liste corrompue.
	_ = cache_service.PurgeUserTimeline(ctx, input.TargetUserID)

	var recoveredPostIDs []int64
	for _, postPayload := range postsFromPostgres {
		// A. On reconstruit le ZSET de la timeline utilisateur de façon rectiligne.
		_ = cache_service.AddPostToUserProfile(ctx, postPayload.UserID, postPayload.ID, float64(postPayload.CreatedAt))

		// B. On blinde la RAM L1 avec les payloads complets.
		_ = object_cache_service.SetPostInObjectCache(ctx, postPayload)

		recoveredPostIDs = append(recoveredPostIDs, postPayload.ID)
	}

	// ── ÉTAPE 4 : RECYCLAGE DU SERVICE PRINCIPAL (100% CACHE HIT GARANTI) ───
	// Capture propre de l'erreur propagée par GetPosts au lieu de forcer "nil"
	outputs, err := GetPosts(ctx, callerID, post_models.GetPostInput{
		PostIDs: recoveredPostIDs,
	})
	return outputs, err
}
