package cache_service

import (
	"context"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
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
// # SERVICE : AMORÇAGE GLOBAL DU SYSTÈME (SEEDING)
// ############################################################################

// SeedMostCache lit l'intégralité de Postgres pour populer le L1 (RAM), L2 (Mongo) et le MOST Cache.
func SeedMostCache(ctx context.Context) error {
	backgroundCtx := context.Background()

	// ── PHASE 1 : RESTAURATION DU SYSTÈME DE TAGS ───────────────────────────

	numan_log.Info(ctx).Msg("Restauration des tags communautaires depuis le Cold Storage SQL...")

	tagsListFromPg, errPgTags := postgres.FuncLoadAllTags(ctx)
	if errPgTags != nil {
		numan_log.Error(ctx).Err(errPgTags).Msg("Échec L3 lors du chargement initial des tags")
	} else if len(tagsListFromPg) > 0 {
		argsForRedis := make([]interface{}, len(tagsListFromPg))
		for index, tagValue := range tagsListFromPg {
			argsForRedis[index] = tagValue
		}
		err := redis.Tags.SAdd(backgroundCtx, "active", argsForRedis...)
		if err != nil {
			appErr := numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeCacheError,
				"Impossible d'initialiser le Set des tags actifs dans Redis.",
				err,
			)
			numan_log.Error(ctx).Err(appErr).Msg("Échec L1 lors de l'ajout des tags actifs dans Redis")
			return appErr
		}
		_ = redis.Tags.SAdd(backgroundCtx, variables.ActiveTagsSubKey(variables.DefaultContentLanguage), argsForRedis...)
	}

	// ── PHASE 2 : HYDRATATION DES POSTS ET CLASSEMENTS (PAR BLOCS) ──────────

	numan_log.Info(ctx).Msg("Hydratation du MOST Cache depuis SQL (Mode Paginé)...")

	paginationLimit := 10000
	paginationOffset := 0
	totalPostsProcessed := 0

	for {
		postsBatchFromPg, errPgPosts := postgres.FuncLoadPostsPaginated(ctx, paginationLimit, paginationOffset)
		if errPgPosts != nil {
			return errPgPosts
		}

		if len(postsBatchFromPg) == 0 {
			break
		}

		for _, postPayload := range postsBatchFromPg {
			// P0 Isolation : Seuls les posts strictement publics (visibility == 0) intègrent le Magasin public
			if postPayload.Visibility != variables.PostVisibilityAll {
				continue
			}

			UpdatePostRecommendationScore(backgroundCtx, postPayload)

			normLang := variables.NormalizeLangForCache(postPayload.Lang)
			if err := redis.RankedPosts.ZAddWithCap(backgroundCtx, variables.StrictRankSubKey(variables.StrictTypeLikes, normLang), float64(postPayload.LikeCount), postPayload.ID, variables.MaxStrictElements); err != nil {
				appErr := numan_error.NewAppError(
					http.StatusInternalServerError,
					numan_error.CodeCacheError,
					"Impossible d'ajouter la publication au classement strict des likes dans Redis.",
					err,
				)
				numan_log.Error(ctx).Err(appErr).Msg("Échec L1 lors de l'ajout du post au ZSET des likes stricts")
				return appErr
			}
			_ = redis.RankedPosts.ZAddWithCap(backgroundCtx, "strict:likes", float64(postPayload.LikeCount), postPayload.ID, variables.MaxStrictElements)

			if err := redis.RankedPosts.ZAddWithCap(backgroundCtx, variables.StrictRankSubKey(variables.StrictTypeViews, normLang), float64(postPayload.ViewCount), postPayload.ID, variables.MaxStrictElements); err != nil {
				appErr := numan_error.NewAppError(
					http.StatusInternalServerError,
					numan_error.CodeCacheError,
					"Impossible d'ajouter la publication au classement strict des vues dans Redis.",
					err,
				)
				numan_log.Error(ctx).Err(appErr).Msg("Échec L1 lors de l'ajout du post au ZSET des vues strictes")
				return appErr
			}
			_ = redis.RankedPosts.ZAddWithCap(backgroundCtx, "strict:views", float64(postPayload.ViewCount), postPayload.ID, variables.MaxStrictElements)
		}

		totalPostsProcessed += len(postsBatchFromPg)
		numan_log.Info(ctx).Int("posts_processed", totalPostsProcessed).Msg("Seeding en cours...")
		paginationOffset += paginationLimit
	}

	// ── PHASE 3 : HYDRATATION INVERSÉE (PRE-WARMING FINAL SANS KEYS BLOQUANT) ─

	numan_log.Info(ctx).Msg("Lancement de l'hydratation inversée (Pre-warming L1/L2 pour l'élite)...")

	winningPostIDsMap := make(map[int64]bool)
	trendKeysList, err := redis.ScanKeys(backgroundCtx, "trend:*", 500)
	if err != nil {
		appErr := numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de récupérer les clés de tendance depuis Redis.",
			err,
		)
		numan_log.Error(ctx).Err(appErr).Msg("Échec L1 lors de la récupération des clés de tendance")
		return appErr
	}

	for _, trendKey := range trendKeysList {
		idStringsInTrend, err := redis.ZRange(backgroundCtx, trendKey, 0, -1)
		if err != nil {
			appErr := numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeCacheError,
				"Impossible de récupérer les IDs de tendance depuis Redis.",
				err,
			)
			numan_log.Error(ctx).Err(appErr).Msg("Échec L1 lors de la récupération des IDs de tendance")
			return appErr
		}
		parsedIDs := pkg.ParseInt64List(idStringsInTrend)
		for _, parsedID := range parsedIDs {
			winningPostIDsMap[parsedID] = true
		}
	}

	if len(winningPostIDsMap) > 0 {
		var winningIDsList []int64
		for id := range winningPostIDsMap {
			winningIDsList = append(winningIDsList, id)
		}

		elitePostsList, errPgElite := postgres.FuncLoadPosts(ctx, winningIDsList, len(winningIDsList), 0)
		if errPgElite != nil {
			numan_log.Warn(ctx).Err(errPgElite).Msg("Échec L3 lors du chargement des posts d'élite")
		} else {
			for _, elitePost := range elitePostsList {
				// L1 : Sanctuarisation immédiate en RAM
				err := object_cache_service.SetPostInObjectCache(backgroundCtx, elitePost)
				if err != nil {
					numan_log.Error(ctx).Err(err).Msg("Échec L1 lors de la sanctuarisation du post d'élite")
					return err
				}

				// L2 : Délégation pour l'insertion par les workers (BulkWrite Mongo)
				err = redis.EnqueueDB(backgroundCtx, elitePost.ID, elitePost.UserID, redis.EntityPost, redis.ActionUpdate, elitePost, redis.TargetMongo)
				if err != nil {
					appErr := numan_error.NewAppError(
						http.StatusInternalServerError,
						numan_error.CodeCacheError,
						"Impossible de mettre en file d'attente le post d'élite pour MongoDB.",
						err,
					)
					numan_log.Error(ctx).Err(appErr).Msg("Échec L1 lors de la mise en file d'attente du post d'élite")
					return appErr
				}
			}
			numan_log.Info(ctx).Int("count", len(elitePostsList)).Msg("Posts d'élite sanctuarisés dans l'Object Cache L1 et en cours d'insertion L2.")
		}
	}

	numan_log.Info(ctx).Msg("Synchronisation MongoDB pour les posts des 30 derniers jours...")
	recentPostsFromPg, errPgRecent := postgres.FuncLoadRecentPosts(ctx, 30)

	if errPgRecent != nil {
		numan_log.Warn(ctx).Err(errPgRecent).Msg("Échec L3 lors du chargement des posts récents")
	} else {
		for _, recentPost := range recentPostsFromPg {
			documentMap, err := pkg.ToMap(recentPost)
			if err != nil {
				numan_log.Warn(ctx).Err(err).Int64("post_id", recentPost.ID).Msg("Échec de la conversion du post récent en map pour MongoDB")
				continue
			}
			if documentMap != nil {
				err = mongo.Posts.Set(documentMap)
				if err != nil {
					numan_log.Warn(ctx).Err(err).Int64("post_id", recentPost.ID).Msg("Échec de la synchronisation du post récent dans le Warm Storage MongoDB")
					continue
				}
			}
		}
		numan_log.Info(ctx).Int("count", len(recentPostsFromPg)).Msg("Posts récents synchronisés dans le Warm Storage MongoDB.")
	}

	// Déverrouillage de l'API
	err = redis.SystemStatus.SetPrimitive(backgroundCtx, "maintenance", "off")
	if err != nil {
		appErr := numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de désactiver le mode maintenance dans Redis.",
			err,
		)
		numan_log.Error(ctx).Err(appErr).Msg("Impossible de désactiver le mode maintenance dans Redis")
		return appErr
	}
	numan_log.Info(ctx).Msg("Mode maintenance désactivé. L'API est opérationnelle.")

	return nil
}

// ############################################################################
// # AMORÇAGE DU SPEED CACHE (Recherche & Relations & Inbox)
// ############################################################################

// seedCommunitySpeedCache charge les communautés publiques dans la barre de recherche.
func seedCommunitySpeedCache(ctx context.Context) error {
	numan_log.Info(ctx).Msg("Amorçage SPEED Cache: Chargement des Communautés Publiques...")

	activeCommunitiesFromPg, errPg := postgres.FuncLoadActiveCommunities(ctx)
	if errPg != nil {
		return errPg
	}

	totalLoaded := 0
	for _, communityPayload := range activeCommunitiesFromPg {
		err := StoreCommunityLiteInSpeedCache(ctx, communityPayload)
		if err != nil {
			numan_log.Warn(ctx).Err(err).Msg("Échec lors du stockage de la communauté dans le SPEED Cache")
			continue
		}
		totalLoaded++
	}

	numan_log.Info(ctx).Int("count", totalLoaded).Msg("SPEED Cache Communautés chargé.")
	return nil
}

// SeedSpeedCache charge les profils allégés et le graphe social relationnel en RAM L1.
func SeedSpeedCache(ctx context.Context) error {
	backgroundCtx := context.Background()
	paginationLimit := 10000

	// ── 1. UTILISATEURS (COMPTES LITE) ──────────────────────────────────────

	numan_log.Info(ctx).Msg("Amorçage SPEED Cache: Chargement des Utilisateurs...")
	offsetUsers := 0
	for {
		usersBatchFromPg, errPg := postgres.FuncLoadUsersPaginated(ctx, paginationLimit, offsetUsers)
		if errPg != nil {
			numan_log.Warn(ctx).Err(errPg).Msg("Erreur L3 lors du chargement paginé des utilisateurs")
			break
		}

		for _, userPayload := range usersBatchFromPg {
			err := storeUserLiteInSpeedCache(backgroundCtx, userPayload)
			if err != nil {
				numan_log.Warn(ctx).Err(err).Int64("user_id", userPayload.ID).Msg("Échec lors du stockage de l'utilisateur dans le SPEED Cache")
				continue
			}
			userLang := variables.DefaultContentLanguage
			if settings, errSet := object_cache_service.GetUserSettingsCascade(backgroundCtx, userPayload.ID); errSet == nil {
				if variables.IsSupportedLanguage(settings.DisplayAndContent.Language) {
					userLang = settings.DisplayAndContent.Language
				}
			}
			_ = AddUserToLanguageIndex(backgroundCtx, userPayload.ID, userLang)
		}

		offsetUsers += len(usersBatchFromPg)
		if len(usersBatchFromPg) < paginationLimit {
			break
		}
	}
	numan_log.Info(ctx).Int("count", offsetUsers).Msg("SPEED Cache Users chargé.")

	// ── 2. GRAPHE SOCIAL (RELATIONS) ────────────────────────────────────────

	numan_log.Info(ctx).Msg("Amorçage SPEED Cache: Chargement des Relations...")
	offsetRelations := 0
	for {
		relationsBatchFromPg, errPg := postgres.FuncLoadRelationsPaginated(ctx, paginationLimit, offsetRelations)
		if errPg != nil {
			numan_log.Warn(ctx).Err(errPg).Msg("Erreur L3 lors du chargement paginé des relations")
			break
		}

		for _, relationRecord := range relationsBatchFromPg {
			err := UpdateRelationState(backgroundCtx, relationRecord.TargetID, relationRecord.CallerID, relationRecord.State, domain.TimeToMillis(relationRecord.CreatedAt))
			if err != nil {
				numan_log.Warn(ctx).Err(err).Int64("callerID", relationRecord.CallerID).Int64("targetID", relationRecord.TargetID).Msg("Échec lors de la mise à jour de la relation dans le SPEED Cache")
				continue
			}
		}

		offsetRelations += len(relationsBatchFromPg)
		if len(relationsBatchFromPg) < paginationLimit {
			break
		}
	}
	numan_log.Info(ctx).Int("count", offsetRelations).Msg("SPEED Cache Relations chargé.")

	// ── 3. COMMUNAUTÉS ──────────────────────────────────────────────────────

	if errComm := seedCommunitySpeedCache(backgroundCtx); errComm != nil {
		numan_log.Warn(ctx).Err(errComm).Msg("Avertissement lors du seeding des communautés")
	}

	// ── 4. MESSAGERIE (INBOX & CHATS) ───────────────────────────────────────

	if errMsg := seedMessagingSpeedCache(backgroundCtx); errMsg != nil {
		numan_log.Warn(ctx).Err(errMsg).Msg("Avertissement lors du seeding de la messagerie")
	}

	// ── 5. ARTICLES MARKDOWN (INDEX L1 NON-ÉVICTABLE) ───────────────────────

	if errArt := seedArticles(backgroundCtx); errArt != nil {
		numan_log.Warn(ctx).Err(errArt).Msg("Avertissement lors du seeding des articles")
	}

	return nil
}

// SeedArticles charge tous les articles actifs depuis PostgreSQL L3 vers l'index Redis L1 persistant (TTL 0) et synchronise MongoDB L2.
func SeedArticles(ctx context.Context) error {
	return seedArticles(ctx)
}

func seedArticles(ctx context.Context) error {
	numan_log.Info(ctx).Msg("Amorçage Articles: Chargement des articles actifs depuis PostgreSQL L3...")

	activeArticles, errPg := postgres.FuncLoadActiveArticles(ctx)
	if errPg != nil {
		return errPg
	}

	if errCache := object_cache_service.SetArticlesBatchInCache(ctx, activeArticles); errCache != nil {
		return errCache
	}

	for _, art := range activeArticles {
		if mongo.Articles != nil && mongo.Articles.DB != nil {
			if errMongo := mongo.MongoUpsertArticle(ctx, art); errMongo != nil {
				numan_log.Warn(ctx).Err(errMongo).Int64("article_id", art.ID).Msg("Échec de la synchronisation de l'article dans MongoDB L2")
			}
		}
	}

	numan_log.Info(ctx).Int("count", len(activeArticles)).Msg("Index Redis L1 des articles initialisé avec succès.")
	return nil
}

// ############################################################################
// # AMORÇAGE DU USER CACHE (TIMELINES)
// ############################################################################

// SeedUserCache reconstruit les chronologies des profils utilisateurs (ZSETs L1).
func SeedUserCache(ctx context.Context) error {
	backgroundCtx := context.Background()
	paginationLimit := 10000
	paginationOffset := 0

	numan_log.Info(ctx).Msg("Amorçage USER Cache: Construction des Timelines L1 (ZSETs)...")

	for {
		timelineSeedsFromPg, errPg := postgres.FuncLoadTimelineSeedPaginated(ctx, paginationLimit, paginationOffset)
		if errPg != nil {
			numan_log.Warn(ctx).Err(errPg).Msg("Erreur L3 lors du chargement des graines de timelines")
			break
		}

		for _, timelineRecord := range timelineSeedsFromPg {
			err := AddPostToUserProfile(backgroundCtx, timelineRecord.UserID, timelineRecord.PostID, float64(domain.TimeToMillis(timelineRecord.CreatedAt)))
			if err != nil {
				numan_log.Warn(ctx).Err(err).Int64("post_id", timelineRecord.PostID).Msg("Échec lors de l'ajout du post à la timeline de l'utilisateur")
				continue
			}
		}

		paginationOffset += len(timelineSeedsFromPg)
		if len(timelineSeedsFromPg) < paginationLimit {
			break
		}
	}

	numan_log.Info(ctx).Int("count", paginationOffset).Msg("USER Cache: Timelines reconstruites avec succès.")
	return nil
}
