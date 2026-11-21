package cache_service

import (
	"context"
	"fmt"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # ROUTINES PRIVÉES D'ACCÈS AUX DONNÉES (HYDRATATION POSTS)
// ############################################################################

// fetchAndHydrateFromCollection abstrait la récupération d'IDs depuis un ZSET Redis
// et lance la cascade d'hydratation L1 -> L2 -> L3 en filtrant strictement les publications publiques (visibility == 0).
func fetchAndHydrateFromCollection(ctx context.Context, redisCollection *redis.Collection, targetKey any, offset int64, limit int64) ([]post_models.PostPayload, error) {
	idStringsList, errRedis := redisCollection.ZRevRange(ctx, targetKey, offset, offset+limit-1)
	if errRedis != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de lire la collection ordonnée dans le cache.",
			errRedis,
		)
	}

	if len(idStringsList) == 0 {
		return []post_models.PostPayload{}, nil
	}

	var parsedIDsList []int64
	for _, idString := range idStringsList {
		var parsedID int64
		_, errScan := fmt.Sscanf(idString, "%d", &parsedID)
		if errScan != nil {
			numan_log.Warn(ctx).Err(errScan).Str("id_string", idString).Msg("Erreur de parsing d'ID dans ZSET")
			continue
		}
		parsedIDsList = append(parsedIDsList, parsedID)
	}

	// Déclenchement de la cascade complète
	posts, err := object_cache_service.GetPostsView(ctx, parsedIDsList)
	if err != nil {
		return nil, err
	}

	// Filet de sécurité P0 : Ne jamais retourner de post non-public depuis un index de classement public
	publicPosts := make([]post_models.PostPayload, 0, len(posts))
	for _, p := range posts {
		if p.Visibility == variables.PostVisibilityAll {
			publicPosts = append(publicPosts, p)
		}
	}
	return publicPosts, nil
}

// getPostsFromMongoPaginated interroge directement le Warm Storage MongoDB (uniquement visibility == 0).
func getPostsFromMongoPaginated(fieldName string, expectedValue any, offset int64, limit int64, lang ...int) ([]post_models.PostPayload, error) {
	mongoFilter := map[string]any{
		fieldName:    expectedValue,
		"visibility": variables.PostVisibilityAll,
	}
	if len(lang) > 0 && variables.IsSupportedLanguage(lang[0]) {
		mongoFilter["lang"] = lang[0]
	}
	mongoSort := map[string]any{"created_at": -1}

	mongoDocuments, errMongo := mongo.Posts.GetPaginated(mongoFilter, mongoSort, offset, limit)
	if errMongo != nil {
		return []post_models.PostPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer les publications paginées depuis MongoDB (L2).",
			errMongo,
		)
	}

	var hydratedPosts []post_models.PostPayload
	for _, document := range mongoDocuments {
		var postPayload post_models.PostPayload
		if errStruct := pkg.ToStruct(document, &postPayload); errStruct == nil {
			if postPayload.Visibility == variables.PostVisibilityAll {
				hydratedPosts = append(hydratedPosts, postPayload)
			}
		} else {
			numan_log.Warn(context.Background()).Err(errStruct).Msg("Échec de conversion BSON vers PostPayload dans getPostsFromMongoPaginated")
		}
	}

	return hydratedPosts, nil
}

// getPostsFromPostgresPaginated est le fallback ultime (Cold Storage L3) pour les classements stricts.
// Respecte la règle DDD #15 (@make-perfect-route) en appelant la procédure stockée via repository/postgres.
func getPostsFromPostgresPaginated(ctx context.Context, rankType string, offset int64, limit int64, lang ...int) ([]post_models.PostPayload, error) {
	orderMode := variables.OrderModeRecent
	switch variables.NormalizeStrictRankType(rankType) {
	case variables.StrictTypeLikes:
		orderMode = variables.OrderModeLikes
	case variables.StrictTypeViews:
		orderMode = variables.OrderModeViews
	default:
		orderMode = variables.OrderModeRecent
	}

	extractedIDs, errPg := postgres.FuncLoadRankedPostIDsPaginated(ctx, orderMode, offset, limit, lang...)
	if errPg != nil {
		return nil, errPg
	}

	return object_cache_service.GetPostsView(ctx, extractedIDs)
}
