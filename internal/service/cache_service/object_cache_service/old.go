package object_cache_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/vmihailenco/msgpack/v5"
)

// ############################################################################
// # PIPELINE D'HYDRATATION GLOBAL : L1 (REDIS) -> L2 (MONGO) -> L3 (POSTGRES)
// ############################################################################

// GetPostsView exécute le pipeline d'hydratation optimisé pour une liste d'IDs de publications.
func GetPostsView(ctx context.Context, targetPostIDs []int64) ([]post_models.PostPayload, error) {
	if len(targetPostIDs) == 0 {
		return []post_models.PostPayload{}, nil
	}

	backgroundCtx := context.Background()
	redisCtx := ctx
	if redisCtx == nil {
		redisCtx = backgroundCtx
	}
	finalHydratedPostsList := make([]post_models.PostPayload, 0, len(targetPostIDs))
	temporaryPostsMap := make(map[int64]post_models.PostPayload)

	// ── ÉTAPE 1 : NIVEAU 1 (REDIS MGET ULTRA-RAPIDE) ────────────────────────
	mgetResult, errMGet := redis.Posts.GetMany(redisCtx, targetPostIDs)
	if errMGet != nil {
		numan_log.Warn(ctx).Err(errMGet).Msg("Erreur Redis MGET (fallback vers L2 Mongo déclenché)")
		mgetResult = &redis.GetManyResult{MissingIDs: targetPostIDs}
	} else {
		for postID, binaryData := range mgetResult.Found {
			var postPayload post_models.PostPayload
			// Décodage du binaire MsgPack
			if errDecode := msgpack.Unmarshal(binaryData, &postPayload); errDecode == nil {
				temporaryPostsMap[postID] = postPayload
			} else {
				numan_log.Warn(ctx).Err(errDecode).Entity("post", postID).Msg("Échec du décodage MessagePack d'un post en L1")
				mgetResult.MissingIDs = append(mgetResult.MissingIDs, postID)
			}
		}
	}

	// ── ÉTAPE 2 : NIVEAU 2 (MONGO FALLBACK WARM STORAGE) ───────────────────
	var stillMissingPostIDs []int64

	if len(mgetResult.MissingIDs) > 0 {
		mongoPostsList, errMongo := mongo.MongoLoadPosts(ctx, mgetResult.MissingIDs)

		if errMongo == nil {
			mongoFoundMap := make(map[int64]bool)

			for _, mongoPost := range mongoPostsList {
				temporaryPostsMap[mongoPost.ID] = mongoPost
				mongoFoundMap[mongoPost.ID] = true

				// PROMOTION L2 -> L1 (Réparation asynchrone du Cache RAM)
				go func(postToPromote post_models.PostPayload) {
					bgRoutineCtx := context.Background()
					if err := SetPostInObjectCache(bgRoutineCtx, postToPromote); err != nil {
						numan_log.Warn(bgRoutineCtx).Err(err).Entity("post", postToPromote.ID).Action(numan_log.ActionHydrate).Msg("Échec de la promotion L2->L1 du post")
					}
				}(mongoPost)
			}

			// Identification de ce qui manque ENCORE après l'étape Mongo
			for _, missingID := range mgetResult.MissingIDs {
				if !mongoFoundMap[missingID] {
					stillMissingPostIDs = append(stillMissingPostIDs, missingID)
				}
			}
		} else {
			numan_log.Error(ctx).Err(errMongo).Msg("Erreur Mongo Fallback (fallback total vers Postgres)")
			stillMissingPostIDs = mgetResult.MissingIDs // Si Mongo plante, on cherchera tout dans Postgres
		}
	}

	// ── ÉTAPE 3 : NIVEAU 3 (POSTGRESQL FALLBACK COLD STORAGE) ──────────────
	if len(stillMissingPostIDs) > 0 {
		numan_log.Info(ctx).Int("missing_count", len(stillMissingPostIDs)).Msg("Postgres Fallback déclenché pour les posts")

		postgresPostsList, errPg := postgres.FuncLoadPosts(ctx, stillMissingPostIDs, len(stillMissingPostIDs), 0)

		if errPg != nil {
			numan_log.Error(ctx).Err(errPg).Msg("Erreur critique Postgres Fallback lors de l'hydratation des posts")
		} else {
			for _, postgresPost := range postgresPostsList {
				temporaryPostsMap[postgresPost.ID] = postgresPost

				// PROMOTION L3 -> L2 & L1 (Auto-guérison complète du système)
				go func(postToHeal post_models.PostPayload) {
					bgRoutineCtx := context.Background()

					// 1. Réparer Redis L1 (Immédiat)
					if err := SetPostInObjectCache(bgRoutineCtx, postToHeal); err != nil {
						numan_log.Warn(bgRoutineCtx).Err(err).Entity("post", postToHeal.ID).Action(numan_log.ActionHydrate).Msg("Échec de la promotion L3->L1 du post")
					}

					// 2. Réparer Mongo L2 (Asynchrone via Worker BulkWrite)
					if err := redis.EnqueueDB(bgRoutineCtx, postToHeal.ID, postToHeal.UserID, redis.EntityPost, redis.ActionUpdate, postToHeal, redis.TargetMongo); err != nil {
						numan_log.Warn(bgRoutineCtx).Err(err).Entity("post", postToHeal.ID).Action(numan_log.ActionSync).Msg("Échec de la mise en file d'attente L3->L2 du post")
					}
				}(postgresPost)
			}
		}
	}

	// ── ÉTAPE 4 : ASSEMBLAGE FINAL STRICT (GARANTIE DE L'ORDRE DES IDs) ─────
	for _, targetID := range targetPostIDs {
		if postPayload, isFound := temporaryPostsMap[targetID]; isFound {
			finalHydratedPostsList = append(finalHydratedPostsList, postPayload)
		}
	}

	return finalHydratedPostsList, nil
}
