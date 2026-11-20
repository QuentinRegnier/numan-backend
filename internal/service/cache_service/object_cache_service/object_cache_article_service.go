package object_cache_service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	goredis "github.com/go-redis/redis/v8"
	"github.com/vmihailenco/msgpack/v5"
)

var rebuildArticlesMu sync.Mutex

// SetArticleInCache enregistre ou met à jour un article dans l'index Redis L1 persistant (TTL = 0 / non-évictable sous volatile-lfu).
// Si l'article a une visibilité < 0 (soft-deleted), il est immédiatement retiré de l'index actif L1.
func SetArticleInCache(ctx context.Context, article article_models.ArticlePayload) error {
	if infraRedis.Rdb == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Le service de cache Redis L1 n'est pas initialisé.",
			errors.New("redis client is nil"),
		)
	}
	if article.ID <= 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'identifiant de l'article est invalide.",
			fmt.Errorf("invalid article ID: %d", article.ID),
		)
	}

	if article.Visibility < 0 {
		return DeleteArticleFromCache(ctx, article.ID)
	}

	article.Normalize()
	rawBytes, errMarshal := msgpack.Marshal(&article)
	if errMarshal != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeInternalError,
			"Impossible de sérialiser l'article pour le cache L1.",
			errMarshal,
		)
	}

	idStr := strconv.FormatInt(article.ID, 10)
	if err := infraRedis.Rdb.HSet(ctx, variables.RedisKeyArticlesIndexData, idStr, string(rawBytes)).Err(); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'écrire les données de l'article dans le HASH Redis L1.",
			err,
		)
	}

	if err := infraRedis.Rdb.ZAdd(ctx, variables.RedisKeyArticlesIndexOrder, &goredis.Z{
		Score:  float64(article.CreatedAt),
		Member: idStr,
	}).Err(); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'indexer l'article dans le ZSET d'ordre Redis L1.",
			err,
		)
	}

	// Marqueur d'initialisation persistant (TTL = 0 -> aucune expiration)
	_ = infraRedis.Rdb.Set(ctx, variables.RedisKeyArticlesIndexInit, "1", 0).Err()
	return nil
}

// SetArticlesBatchInCache enregistre un lot d'articles actifs dans l'index Redis L1 persistant (TTL = 0).
func SetArticlesBatchInCache(ctx context.Context, articles []article_models.ArticlePayload) error {
	if infraRedis.Rdb == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Le service de cache Redis L1 n'est pas initialisé.",
			errors.New("redis client is nil"),
		)
	}

	for _, a := range articles {
		if a.ID <= 0 {
			continue
		}
		if a.Visibility < 0 {
			_ = DeleteArticleFromCache(ctx, a.ID)
			continue
		}
		if err := SetArticleInCache(ctx, a); err != nil {
			return err
		}
	}

	if err := infraRedis.Rdb.Set(ctx, variables.RedisKeyArticlesIndexInit, "1", 0).Err(); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de marquer l'index des articles comme initialisé dans Redis L1.",
			err,
		)
	}
	return nil
}

// DeleteArticleFromCache retire un article de l'index Redis L1 (HASH data + ZSET order).
func DeleteArticleFromCache(ctx context.Context, articleID int64) error {
	if infraRedis.Rdb == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Le service de cache Redis L1 n'est pas initialisé.",
			errors.New("redis client is nil"),
		)
	}
	if articleID <= 0 {
		return nil
	}

	idStr := strconv.FormatInt(articleID, 10)
	if err := infraRedis.Rdb.HDel(ctx, variables.RedisKeyArticlesIndexData, idStr).Err(); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de supprimer l'article du HASH Redis L1.",
			err,
		)
	}
	if err := infraRedis.Rdb.ZRem(ctx, variables.RedisKeyArticlesIndexOrder, idStr).Err(); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de supprimer l'article du ZSET d'ordre Redis L1.",
			err,
		)
	}
	return nil
}

// DeleteArticlesBatchFromCache retire un lot d'articles de l'index Redis L1.
func DeleteArticlesBatchFromCache(ctx context.Context, articleIDs []int64) error {
	for _, id := range articleIDs {
		if err := DeleteArticleFromCache(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// GetArticleFromCache récupère un article par son ID avec cascade L1 (Redis) -> L2 (MongoDB) -> L3 (PostgreSQL) et auto-réparation.
func GetArticleFromCache(ctx context.Context, articleID int64) (article_models.ArticlePayload, error) {
	if articleID <= 0 {
		return article_models.ArticlePayload{}, numan_error.NewNotFound(
			numan_error.CodeNotFound,
			"Article introuvable.",
			fmt.Errorf("invalid article ID: %d", articleID),
		)
	}

	idStr := strconv.FormatInt(articleID, 10)

	// 1. Tentative L1 (Redis HASH)
	if infraRedis.Rdb != nil {
		rawStr, errHGet := infraRedis.Rdb.HGet(ctx, variables.RedisKeyArticlesIndexData, idStr).Result()
		if errHGet == nil && len(rawStr) > 0 {
			var article article_models.ArticlePayload
			if errUnmarshal := msgpack.Unmarshal([]byte(rawStr), &article); errUnmarshal == nil && article.ID > 0 {
				article.Normalize()
				if article.Visibility < 0 {
					_ = DeleteArticleFromCache(ctx, articleID)
					return article_models.ArticlePayload{}, numan_error.NewNotFound(
						numan_error.CodeNotFound,
						"Article introuvable.",
						fmt.Errorf("article %d is soft-deleted", articleID),
					)
				}
				return article, nil
			}
			// Payload corrompu : on poursuit en cascade L2/L3 pour auto-réparer
			numan_log.Warn(ctx).Int64("article_id", articleID).Msg("Article L1 MsgPack corrompu, déclenchement cascade L2/L3")
		}
	}

	// 2. Tentative L2 (MongoDB)
	artL2, foundL2, errL2 := mongo.MongoGetArticleByID(ctx, articleID)
	if errL2 == nil && foundL2 && artL2.ID > 0 {
		if artL2.Visibility < 0 {
			_ = DeleteArticleFromCache(ctx, articleID)
			return article_models.ArticlePayload{}, numan_error.NewNotFound(
				numan_error.CodeNotFound,
				"Article introuvable.",
				fmt.Errorf("article %d is soft-deleted in L2", articleID),
			)
		}
		_ = SetArticleInCache(ctx, artL2)
		return artL2, nil
	}

	// 3. Tentative L3 (PostgreSQL)
	artL3, foundL3, errL3 := postgres.FuncGetArticleByID(ctx, articleID)
	if errL3 == nil && foundL3 && artL3.ID > 0 {
		if artL3.Visibility < 0 {
			_ = DeleteArticleFromCache(ctx, articleID)
			return article_models.ArticlePayload{}, numan_error.NewNotFound(
				numan_error.CodeNotFound,
				"Article introuvable.",
				fmt.Errorf("article %d is soft-deleted in L3", articleID),
			)
		}
		_ = SetArticleInCache(ctx, artL3)
		_ = mongo.MongoUpsertArticle(ctx, artL3)
		return artL3, nil
	}

	return article_models.ArticlePayload{}, numan_error.NewNotFound(
		numan_error.CodeNotFound,
		"Article introuvable.",
		fmt.Errorf("article %d not found in L1/L2/L3", articleID),
	)
}

// ListArticlesFromCache retourne tous les articles actifs depuis l'index Redis L1, triés par created_at DESC puis id DESC.
// Si l'index n'est pas initialisé ou désynchronisé, il déclenche une auto-réparation depuis L2/L3.
func ListArticlesFromCache(ctx context.Context) ([]article_models.ArticlePayload, error) {
	if infraRedis.Rdb == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Le service de cache Redis L1 n'est pas initialisé.",
			errors.New("redis client is nil"),
		)
	}

	idStrs, errZ := infraRedis.Rdb.ZRevRange(ctx, variables.RedisKeyArticlesIndexOrder, 0, -1).Result()
	if errZ != nil && !errors.Is(errZ, goredis.Nil) {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de lire le ZSET d'ordre des articles dans Redis L1.",
			errZ,
		)
	}

	if len(idStrs) == 0 {
		// Vérifie si l'index a déjà été initialisé (vide légitime vs Redis non initialisé)
		initCount, errInit := infraRedis.Rdb.Exists(ctx, variables.RedisKeyArticlesIndexInit).Result()
		if errInit == nil && initCount > 0 {
			// Vérifie également si le HASH data contient des entrées orphelines malgré un ZSET vide
			hLen, errHLen := infraRedis.Rdb.HLen(ctx, variables.RedisKeyArticlesIndexData).Result()
			if errHLen == nil && hLen == 0 {
				return []article_models.ArticlePayload{}, nil
			}
		}
		return RebuildArticlesIndex(ctx)
	}

	rawVals, errHMGet := infraRedis.Rdb.HMGet(ctx, variables.RedisKeyArticlesIndexData, idStrs...).Result()
	if errHMGet != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de lire les payloads d'articles depuis le HASH Redis L1.",
			errHMGet,
		)
	}

	results := make([]article_models.ArticlePayload, 0, len(idStrs))
	var missingIDs []int64

	for i, idStr := range idStrs {
		parsedID, errParse := strconv.ParseInt(idStr, 10, 64)
		if errParse != nil || parsedID <= 0 {
			_ = infraRedis.Rdb.ZRem(ctx, variables.RedisKeyArticlesIndexOrder, idStr).Err()
			continue
		}

		var rawStr string
		if i < len(rawVals) && rawVals[i] != nil {
			if s, ok := rawVals[i].(string); ok {
				rawStr = s
			}
		}

		if len(rawStr) == 0 {
			missingIDs = append(missingIDs, parsedID)
			continue
		}

		var art article_models.ArticlePayload
		if errUnmarshal := msgpack.Unmarshal([]byte(rawStr), &art); errUnmarshal != nil || art.ID <= 0 {
			missingIDs = append(missingIDs, parsedID)
			continue
		}
		art.Normalize()

		if art.Visibility < 0 {
			_ = DeleteArticleFromCache(ctx, art.ID)
			continue
		}
		results = append(results, art)
	}

	// Auto-réparation des entrées présentes dans ZSET mais absentes ou corrompues dans HASH
	if len(missingIDs) > 0 {
		healed := healMissingArticlesByIDs(ctx, missingIDs)
		results = append(results, healed...)
	}

	sortArticlesDescending(results)
	return results, nil
}

// healMissingArticlesByIDs récupère les articles manquants en batch depuis L2 puis L3,
// répare les entrées actives dans L1 et purge les membres ZSET orphelins ou supprimés.
func healMissingArticlesByIDs(ctx context.Context, missingIDs []int64) []article_models.ArticlePayload {
	recoveredMap := make(map[int64]article_models.ArticlePayload, len(missingIDs))

	if l2List, errL2 := mongo.MongoLoadArticlesByIDs(ctx, missingIDs); errL2 == nil {
		for _, a := range l2List {
			if a.ID > 0 {
				recoveredMap[a.ID] = a
			}
		}
	}

	var stillMissing []int64
	for _, id := range missingIDs {
		if _, ok := recoveredMap[id]; !ok {
			stillMissing = append(stillMissing, id)
		}
	}

	if len(stillMissing) > 0 {
		if l3List, errL3 := postgres.FuncGetArticlesByIDs(ctx, stillMissing); errL3 == nil {
			for _, a := range l3List {
				if a.ID > 0 {
					recoveredMap[a.ID] = a
					_ = mongo.MongoUpsertArticle(ctx, a)
				}
			}
		}
	}

	healed := make([]article_models.ArticlePayload, 0, len(recoveredMap))
	for _, id := range missingIDs {
		art, ok := recoveredMap[id]
		if !ok || art.Visibility < 0 {
			// Article inexistant ou soft-deleted : retirer l'entrée orpheline de L1
			_ = DeleteArticleFromCache(ctx, id)
			continue
		}
		art.Normalize()
		_ = SetArticleInCache(ctx, art)
		healed = append(healed, art)
	}
	return healed
}

// RebuildArticlesIndex reconstruit intégralement l'index Redis L1 des articles actifs depuis L3 (ou L2 en fallback),
// avec protection singleflight / verrou Redis contre les tempêtes de reconstruction.
func RebuildArticlesIndex(ctx context.Context) ([]article_models.ArticlePayload, error) {
	if infraRedis.Rdb == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Le service de cache Redis L1 n'est pas initialisé.",
			errors.New("redis client is nil"),
		)
	}

	rebuildArticlesMu.Lock()
	defer rebuildArticlesMu.Unlock()

	// Double-check après acquisition du mutex local : si une goroutine concurrente vient de reconstruire l'index
	if initCount, errInit := infraRedis.Rdb.Exists(ctx, variables.RedisKeyArticlesIndexInit).Result(); errInit == nil && initCount > 0 {
		idStrs, errZ := infraRedis.Rdb.ZRevRange(ctx, variables.RedisKeyArticlesIndexOrder, 0, -1).Result()
		hLen, errH := infraRedis.Rdb.HLen(ctx, variables.RedisKeyArticlesIndexData).Result()
		if errZ == nil && errH == nil && int64(len(idStrs)) == hLen {
			if len(idStrs) == 0 {
				return []article_models.ArticlePayload{}, nil
			}
			// Lecture rapide de l'index déjà reconstruit
			if rawVals, errHM := infraRedis.Rdb.HMGet(ctx, variables.RedisKeyArticlesIndexData, idStrs...).Result(); errHM == nil {
				cached := make([]article_models.ArticlePayload, 0, len(idStrs))
				allValid := true
				for _, rv := range rawVals {
					s, ok := rv.(string)
					if !ok || len(s) == 0 {
						allValid = false
						break
					}
					var a article_models.ArticlePayload
					if errU := msgpack.Unmarshal([]byte(s), &a); errU != nil || a.ID <= 0 {
						allValid = false
						break
					}
					a.Normalize()
					if a.Visibility >= 0 {
						cached = append(cached, a)
					}
				}
				if allValid {
					sortArticlesDescending(cached)
					return cached, nil
				}
			}
		}
	}

	// Acquisition optionnelle du verrou distribué Redis (TTL court de 10s pour éviter tout deadlock)
	_ = infraRedis.Rdb.SetNX(ctx, variables.RedisKeyArticlesRebuildLck, "1", 10*time.Second).Err()
	defer func() {
		_ = infraRedis.Rdb.Del(ctx, variables.RedisKeyArticlesRebuildLck).Err()
	}()

	// 1. Chargement depuis PostgreSQL L3 (Source de vérité) avec fallback sur MongoDB L2
	var activeArticles []article_models.ArticlePayload
	l3Articles, errL3 := postgres.FuncLoadActiveArticles(ctx)
	if errL3 == nil && len(l3Articles) > 0 {
		activeArticles = l3Articles
	} else {
		l2Articles, errL2 := mongo.MongoLoadActiveArticles(ctx)
		if errL2 == nil && len(l2Articles) > 0 {
			activeArticles = l2Articles
		} else if errL3 != nil && errL2 != nil {
			// Si L3 et L2 sont tous deux en échec, retourner une liste vide sans marquer l'index comme initialisé
			numan_log.Warn(ctx).Err(errL3).Msg("RebuildArticlesIndex : L3 et L2 indisponibles")
			return []article_models.ArticlePayload{}, nil
		}
	}

	// Nettoyage des anciennes clés d'index avant reconstruction propre
	_ = infraRedis.Rdb.Del(ctx, variables.RedisKeyArticlesIndexData, variables.RedisKeyArticlesIndexOrder).Err()

	validArticles := make([]article_models.ArticlePayload, 0, len(activeArticles))
	for _, art := range activeArticles {
		if art.ID <= 0 || art.Visibility < 0 {
			continue
		}
		art.Normalize()
		if err := SetArticleInCache(ctx, art); err != nil {
			return nil, err
		}
		validArticles = append(validArticles, art)
	}

	// Marquer l'index comme initialisé même s'il y a 0 article actif (évite les rebuild storms sur base vide)
	if err := infraRedis.Rdb.Set(ctx, variables.RedisKeyArticlesIndexInit, "1", 0).Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'enregistrer le marqueur d'initialisation de l'index des articles.",
			err,
		)
	}

	sortArticlesDescending(validArticles)
	return validArticles, nil
}

func sortArticlesDescending(articles []article_models.ArticlePayload) {
	sort.Slice(articles, func(i, j int) bool {
		if articles[i].CreatedAt != articles[j].CreatedAt {
			return articles[i].CreatedAt > articles[j].CreatedAt
		}
		return articles[i].ID > articles[j].ID
	})
}
