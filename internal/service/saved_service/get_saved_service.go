package saved_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/saved_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/post_service"
)

// ############################################################################
// # SERVICE : RÉCUPÉRATION DES PUBLICATIONS SAUVEGARDÉES
// ############################################################################

// GetSavedPosts récupère la liste paginée des favoris d'un utilisateur
// avec la garantie d'auto-guérison en cascade L1 -> L2 -> L3.
func GetSavedPosts(ctx context.Context, callerID int64, input saved_models.GetSavedInput) ([]post_models.GetPostOutput, error) {
	var targetPostIDs []int64

	// ── ÉTAPE 1 : TENTATIVE L1 (RAM REDIS ZSET) ─────────────────────────────

	cachedPostIDs, errCache := object_cache_service.GetSavedPostIDs(ctx, callerID, input.Offset, input.Limit)
	if errCache == nil && len(cachedPostIDs) > 0 {
		targetPostIDs = cachedPostIDs
	} else {

		// ── ÉTAPE 2 : FALLBACK L2 (MONGODB WARM STORAGE) ────────────────────
		savedPayloadsFromMongo, errMongo := mongo.MongoLoadSavedPosts(ctx, callerID, input.Limit, input.Offset)

		if errMongo == nil && len(savedPayloadsFromMongo) > 0 {
			for _, savedPayload := range savedPayloadsFromMongo {
				targetPostIDs = append(targetPostIDs, savedPayload.PostID)

				// AUTO-GUÉRISON L1 (ZSET)
				_ = object_cache_service.AddSavedToZSET(ctx, callerID, savedPayload.PostID, float64(savedPayload.CreatedAt))
			}
		} else {

			// ── ÉTAPE 3 : FALLBACK ABSOLU L3 (POSTGRESQL COLD STORAGE) ──────
			savedPayloadsFromPostgres, errPg := postgres.FuncLoadSavedPostsKeyset(ctx, callerID, input.BeforeCreatedAt, input.BeforeID, input.Limit)
			if errPg != nil {
				numan_log.Error(ctx).Err(errPg).Int64("user_id", callerID).Msg("Erreur L3 lors de la récupération des posts sauvegardés")
				return nil, numan_error.NewInternal()
			}

			if len(savedPayloadsFromPostgres) > 0 {
				for _, savedPayload := range savedPayloadsFromPostgres {
					targetPostIDs = append(targetPostIDs, savedPayload.PostID)

					// AUTO-GUÉRISON L1 (Synchrone en RAM)
					_ = object_cache_service.AddSavedToZSET(ctx, callerID, savedPayload.PostID, float64(savedPayload.CreatedAt))

					// AUTO-GUÉRISON L2 (Asynchrone via Workers)
					go func(payload saved_models.SavedPayload) {
						backgroundCtx := context.Background()
						// PartitionKey = UserID pour centraliser les favoris d'un même utilisateur
						_ = redis.EnqueueDB(backgroundCtx, payload.ID, payload.UserID, redis.EntitySaved, redis.ActionUpdate, payload, redis.TargetMongo)
					}(savedPayload)
				}
			}
		}
	}

	// ── ÉTAPE 4 : COUPE-CIRCUIT (AUCUN FAVORI) ──────────────────────────────

	if len(targetPostIDs) == 0 {
		return []post_models.GetPostOutput{}, nil
	}

	// ── ÉTAPE 5 : DÉLÉGATION AU POST_SERVICE POUR HYDRATATION MASSIVE ───────

	// On passe la liste d'IDs au pipeline unifié GetPosts qui va s'occuper :
	// - De vérifier les droits d'accès
	// - De filtrer les posts supprimés ou restreints (abonnés/amis)
	// - De générer les URLs S3/MinIO signées (HMAC)
	// - D'hydrater les commentaires
	requestInput := post_models.GetPostInput{
		PostIDs: targetPostIDs,
	}

	hydratedPostsResults, err := post_service.GetPosts(ctx, callerID, requestInput)
	if err != nil {
		numan_log.Error(ctx).Err(err).Int64("user_id", callerID).Msg("Erreur lors de l'hydratation des posts sauvegardés")
		return nil, numan_error.NewInternal()
	}

	return hydratedPostsResults, nil
}
