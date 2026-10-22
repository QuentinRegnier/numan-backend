package cache_service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
)

// ############################################################################
// # ROUTINES PRIVÉES D'ACCÈS AUX DONNÉES (HYDRATATION POSTS)
// ############################################################################

// fetchAndHydrateFromCollection abstrait la récupération d'IDs depuis un ZSET Redis
// et lance la cascade d'hydratation L1 -> L2 -> L3.
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
	return object_cache_service.GetPostsView(ctx, parsedIDsList)
}

// getPostsFromMongoPaginated interroge directement le Warm Storage MongoDB.
func getPostsFromMongoPaginated(fieldName string, expectedValue any, offset int64, limit int64) ([]post_models.PostPayload, error) {
	mongoFilter := map[string]any{fieldName: expectedValue}
	mongoSort := map[string]any{"created_at": -1}

	mongoDocuments, errMongo := mongo.Posts.GetPaginated(mongoFilter, mongoSort, offset, limit)
	if errMongo != nil {
		return []post_models.PostPayload{}, errMongo
	}

	var hydratedPosts []post_models.PostPayload
	for _, document := range mongoDocuments {
		var postPayload post_models.PostPayload
		if errStruct := pkg.ToStruct(document, &postPayload); errStruct == nil {
			hydratedPosts = append(hydratedPosts, postPayload)
		}
	}

	return hydratedPosts, nil
}

// getPostsFromPostgresPaginated est le fallback ultime (Cold Storage) pour les classements.
func getPostsFromPostgresPaginated(ctx context.Context, rankType string, offset int64, limit int64) ([]post_models.PostPayload, error) {
	var sqlQuery string

	// TODO: Optimiser ces requêtes avec des vues matérialisées si la BDD dépasse 1M de lignes
	switch rankType {
	case "likes:strict":
		sqlQuery = `
			SELECT p.id FROM content.posts p 
			WHERE p.visibility != 2 
			ORDER BY (SELECT COUNT(*) FROM content.likes l WHERE l.target_id = p.id AND l.target_type = 0) DESC, p.created_at DESC 
			OFFSET $1 LIMIT $2`
	case "views:strict":
		sqlQuery = `
			SELECT p.id FROM content.posts p 
			WHERE p.visibility != 2 
			ORDER BY (SELECT COUNT(*) FROM content.views v WHERE v.target_id = p.id AND v.target_type = 0) DESC, p.created_at DESC 
			OFFSET $1 LIMIT $2`
	default:
		sqlQuery = `SELECT id FROM content.posts WHERE visibility != 2 ORDER BY created_at DESC OFFSET $1 LIMIT $2`
	}

	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	sqlRows, errPg := postgres.PostgresDB.QueryContext(ctx, sqlQuery, offset, limit)
	if errPg != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer les classements de publications depuis PostgreSQL (L3).",
			errPg,
		)
	}

	defer func(rowsToClose *sql.Rows) {
		if errClose := rowsToClose.Close(); errClose != nil {
			numan_log.Warn(ctx).Err(errClose).Msg("Erreur lors de la fermeture du curseur Rows L3 Postgres")
		}
	}(sqlRows)

	var extractedIDs []int64
	for sqlRows.Next() {
		var extractedID int64
		if errScan := sqlRows.Scan(&extractedID); errScan == nil {
			extractedIDs = append(extractedIDs, extractedID)
		}
	}

	return object_cache_service.GetPostsView(ctx, extractedIDs)
}
