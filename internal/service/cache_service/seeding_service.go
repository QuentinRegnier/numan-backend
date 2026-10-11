package cache_service

import (
	"context"

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
			numan_log.Error(ctx).Err(err).Msg("Échec L3 lors de l'ajout des tags au ZSET")
			return err
		}
	}

	// ── PHASE 2 : HYDRATATION DES POSTS ET CLASSEMENTS (PAR BLOCS) ──────────

	numan_log.Info(ctx).Msg("Hydratation du MOST Cache depuis SQL (Mode Paginé)...")

	paginationLimit := 10000
	paginationOffset := 0
	totalPostsProcessed := 0

	for {
		postsBatchFromPg, errPgPosts := postgres.FuncLoadPostsPaginated(ctx, paginationLimit, paginationOffset)
		if errPgPosts != nil {
			numan_log.Error(ctx).Err(errPgPosts).Msg("Échec L3 lors du seeding paginé des posts")
			return numan_error.NewInternal()
		}

		if len(postsBatchFromPg) == 0 {
			break
		}

		for _, postPayload := range postsBatchFromPg {
			UpdatePostRecommendationScore(backgroundCtx, postPayload)

			err := redis.ZAddWithCap(backgroundCtx, variables.RedisKeyStrictLikes, float64(postPayload.LikeCount), postPayload.ID, variables.MaxStrictElements)
			if err != nil {
				numan_log.Error(ctx).Err(err).Msg("Échec L3 lors de l'ajout du post au ZSET des likes stricts")
				return err
			}
			err = redis.ZAddWithCap(backgroundCtx, variables.RedisKeyStrictViews, float64(postPayload.ViewCount), postPayload.ID, variables.MaxStrictElements)
			if err != nil {
				numan_log.Error(ctx).Err(err).Msg("Échec L3 lors de l'ajout du post au ZSET des vues strictes")
				return err
			}
		}

		totalPostsProcessed += len(postsBatchFromPg)
		numan_log.Info(ctx).Int("posts_processed", totalPostsProcessed).Msg("Seeding en cours...")
		paginationOffset += paginationLimit
	}

	// ── PHASE 3 : HYDRATATION INVERSÉE (PRE-WARMING FINAL) ──────────────────

	numan_log.Info(ctx).Msg("Lancement de l'hydratation inversée (Pre-warming L1/L2 pour l'élite)...")

	winningPostIDsMap := make(map[int64]bool)
	trendKeysList, err := redis.Keys(backgroundCtx, "most_cache:trend:*")
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Échec L3 lors de la récupération des clés de tendance")
		return err
	}

	for _, trendKey := range trendKeysList {
		idStringsInTrend, err := redis.ZRange(backgroundCtx, trendKey, 0, -1)
		if err != nil {
			numan_log.Error(ctx).Err(err).Msg("Échec L3 lors de la récupération des IDs de tendance")
			return err
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
		if errPgElite == nil {
			for _, elitePost := range elitePostsList {
				// L1 : Sanctuarisation immédiate en RAM
				err := object_cache_service.SetPostInObjectCache(backgroundCtx, elitePost)
				if err != nil {
					numan_log.Error(ctx).Err(err).Msg("Échec L3 lors de la sanctuarisation du post d'élite")
					return err
				}

				// L2 : Délégation pour l'insertion par les workers (BulkWrite Mongo)
				err = redis.EnqueueDB(backgroundCtx, elitePost.ID, elitePost.UserID, redis.EntityPost, redis.ActionUpdate, elitePost, redis.TargetMongo)
				if err != nil {
					numan_log.Error(ctx).Err(err).Msg("Échec L3 lors de l'insertion du post d'élite")
					return err
				}
			}
			numan_log.Info(ctx).Int("count", len(elitePostsList)).Msg("Posts d'élite sanctuarisés dans l'Object Cache L1 et en cours d'insertion L2.")
		}
	}

	numan_log.Info(ctx).Msg("Synchronisation MongoDB pour les posts des 30 derniers jours...")
	recentPostsFromPg, errPgRecent := postgres.FuncLoadRecentPosts(ctx, 30)

	if errPgRecent == nil {
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
		numan_log.Error(ctx).Err(err).Msg("Impossible de désactiver le mode maintenance dans Redis")
		return err
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
		return numan_error.NewInternal()
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
			err := AddPostToUserProfile(backgroundCtx, timelineRecord.UserID, timelineRecord.PostID, float64(timelineRecord.CreatedAt.UnixMilli()))
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
