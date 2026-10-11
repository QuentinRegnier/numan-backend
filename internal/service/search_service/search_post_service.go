package search_service

import (
	"context"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/search_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/post_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : RECHERCHE TEXTUELLE & GLOBALE DE PUBLICATIONS
// ############################################################################

// mapFilterToOrderMode traduit la string du front-end en entier compréhensible par SQL.
func mapFilterToOrderMode(frontendFilter string) int {
	switch frontendFilter {
	case variables.SearchFilterViews:
		return variables.OrderModeViews
	case variables.SearchFilterLikes:
		return variables.OrderModeLikes
	case variables.SearchFilterComments:
		return variables.OrderModeComments
	case variables.SearchFilterRecent:
		return variables.OrderModeRecent
	case variables.SearchFilterOldest:
		return variables.OrderModeOldest
	default:
		return variables.OrderModeRecent // Recent par défaut pour éviter le chaos
	}
}

// SearchPosts est le moteur de routage principal des recherches complexes.
func SearchPosts(ctx context.Context, callerID int64, input search_models.SearchPostInput) (search_models.SearchPostOutput, error) {
	var err_offset, errLimit numan_error.Error
	input.Offset, err_offset, input.Limit, errLimit = pkg.BatchVerif(input.Offset, input.Limit)
	if err_offset != nil || errLimit != nil {
		return search_models.SearchPostOutput{}, numan_error.Combine(err_offset, errLimit)
	}

	var resolvedPostIDs []int64
	hasQueryBeenIntercepted := false
	targetOrderMode := mapFilterToOrderMode(input.Filter)

	// ── ÉTAPE 1 : DÉCOUVERTE GLOBALE (MOST CACHE L1) ────────────────────────

	// Si l'utilisateur clique sur un filtre sans faire de recherche textuelle.
	if input.Query == "" && input.Filter != "" && input.Filter != variables.SearchFilterTrend {
		targetRankType := ""

		switch input.Filter {
		case variables.SearchFilterLikes:
			targetRankType = "strict:likes"
		case variables.SearchFilterViews:
			targetRankType = "strict:views"
		case variables.SearchFilterRecent:
			targetRankType = "strict:recent"
		}

		if targetRankType != "" {
			rankedPayloadsFromCache, errRedis := cache_service.GetRankedPosts(ctx, targetRankType, input.Offset, input.Limit)
			if errRedis != nil {
				numan_log.Error(ctx).Err(errRedis).Str("rank_type", targetRankType).Msg("Erreur L1 lors de la récupération des tops posts")
				return search_models.SearchPostOutput{}, numan_error.NewInternal()
			}

			for _, payload := range rankedPayloadsFromCache {
				resolvedPostIDs = append(resolvedPostIDs, payload.ID)
			}
			hasQueryBeenIntercepted = true
		}
	}

	// ── ÉTAPE 2 : RECHERCHE PAR HASHTAG EXACT (#) ───────────────────────────

	if !hasQueryBeenIntercepted && strings.HasPrefix(input.Query, "#") {
		cleanHashtagSlug := service.NormalizeHashtag(ctx, input.Query)

		if cleanHashtagSlug != "" {
			// Si on demande les plus récents, on tape le ZSET ultra-rapide L1
			if targetOrderMode == variables.OrderModeRecent {
				tagPayloadsFromCache, errCache := cache_service.GetTagPosts(ctx, cleanHashtagSlug, input.Offset, input.Limit)
				if errCache == nil {
					for _, payload := range tagPayloadsFromCache {
						resolvedPostIDs = append(resolvedPostIDs, payload.ID)
					}
					hasQueryBeenIntercepted = true
				}
			} else {
				// Si on veut trier ("Le tag #nature le plus liké"), on DOIT déléguer au L3
				pgIDs, errPg := postgres.FuncSearchPostIDsByTag(ctx, cleanHashtagSlug, targetOrderMode, input.Offset, input.Limit)
				if errPg == nil {
					resolvedPostIDs = append(resolvedPostIDs, pgIDs...)
					hasQueryBeenIntercepted = true
				} else {
					numan_log.Warn(ctx).Err(errPg).Msg("Échec L3 de la recherche de Post par Tag trié")
				}
			}
		}
	}

	// ── ÉTAPE 3 : RECHERCHE PAR AUTEUR EXACT (@) ────────────────────────────

	if !hasQueryBeenIntercepted && strings.HasPrefix(input.Query, "@") {
		targetUsername := strings.ToLower(strings.TrimPrefix(input.Query, "@"))

		// Recherche instantanée L1 Lex pour trouver l'ID de l'auteur ciblé
		usersList, errCache := cache_service.SearchUserByPrefix(ctx, targetUsername, 0, 1)

		if errCache == nil && len(usersList) > 0 && strings.ToLower(usersList[0].Username) == targetUsername {
			pgIDs, errPg := postgres.FuncSearchPostIDsByUsers(ctx, []int64{usersList[0].ID}, targetOrderMode, input.Offset, input.Limit)
			if errPg == nil {
				resolvedPostIDs = append(resolvedPostIDs, pgIDs...)
				hasQueryBeenIntercepted = true
			} else {
				numan_log.Warn(ctx).Err(errPg).Msg("Échec L3 de la recherche de Post par Auteur Exact")
			}
		}
	}

	// ── ÉTAPE 4 : FALLBACK FULL-TEXT SEARCH (POSTGRESQL L3) ─────────────────

	if !hasQueryBeenIntercepted && input.Query != "" {
		pgIDs, errPg := postgres.FuncSearchPostIDsByText(ctx, input.Query, targetOrderMode, input.Offset, input.Limit)
		if errPg != nil {
			numan_log.Error(ctx).Err(errPg).Str("query", input.Query).Msg("Erreur critique L3 lors de la Full-Text Search des posts")
			return search_models.SearchPostOutput{}, numan_error.NewInternal()
		}
		resolvedPostIDs = append(resolvedPostIDs, pgIDs...)
	}

	// ── ÉTAPE 5 : COUPE-CIRCUIT (AUCUN RÉSULTAT) ────────────────────────────

	if len(resolvedPostIDs) == 0 {
		return search_models.SearchPostOutput{
			Posts:                   make([]post_models.GetPostOutput, 0),
			HasTrendFilterAvailable: true,
		}, nil
	}

	// ── ÉTAPE 6 : HYDRATATION FINALE UNIFIÉE VIA LE DOMAINE POST ────────────

	hydrationRequestInput := post_models.GetPostInput{
		PostIDs: resolvedPostIDs,
	}

	fullyHydratedPostsResults, err := post_service.GetPosts(ctx, callerID, hydrationRequestInput)
	if err != nil {
		numan_log.Error(ctx).Err(err).Int64("caller_id", callerID).Int("num_posts", len(resolvedPostIDs)).Msg("Échec de l'hydratation finale des posts recherchés")
		return search_models.SearchPostOutput{}, numan_error.NewInternal(err)
	}

	return search_models.SearchPostOutput{
		Posts:                   fullyHydratedPostsResults,
		HasTrendFilterAvailable: true,
	}, nil
}
