package search_service

import (
	"context"
	"sort"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/search_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : AUTOCOMPLÉTION DES HASHTAGS
// ############################################################################

func AutocompleteTags(ctx context.Context, input search_models.AutocompleteTagInput) (search_models.AutocompleteTagOutput, error) {
	if err := input.Validate(); err != nil {
		return search_models.AutocompleteTagOutput{}, err
	}
	if input.Limit == 0 {
		input.Limit = variables.DefaultAutocompleteLimit
		if input.Limit <= 0 {
			input.Limit = 15
		}
	}

	sanitizedQuery := strings.ToLower(strings.TrimSpace(input.Query))
	var finalTagResults []string
	alreadySeenTagsMap := make(map[string]bool)

	// ── ÉTAPE 1 : RECHERCHE LEXICOGRAPHIQUE L1 (O(log N)) ───────────────────
	// ATTENTION : Il faudra mettre à jour la signature dans cache_service pour passer input.Offset
	lexicographicTags, errRedis := cache_service.SearchTagsByPrefix(ctx, sanitizedQuery, input.Offset, input.Limit)
	if errRedis != nil {
		numan_log.Error(ctx).Err(errRedis).Str("query", sanitizedQuery).Msg("Erreur L1 lors de la recherche lexicographique des tags")
		return search_models.AutocompleteTagOutput{}, numan_error.NewInternal()
	}

	isExactTagMatch := false
	for _, tag := range lexicographicTags {
		if tag == sanitizedQuery {
			isExactTagMatch = true
			break
		}
	}

	// ── ÉTAPE 2 : MAGIE SÉMANTIQUE (GRAPHE DE MARKOV) ───────────────────────
	// NOUVEAU : Exécuté UNIQUEMENT sur la première page (Offset == 0)
	if isExactTagMatch && input.Offset == 0 {
		finalTagResults = append(finalTagResults, sanitizedQuery)
		alreadySeenTagsMap[sanitizedQuery] = true

		semanticallyRelatedTagsMap := cache_service.GetRelatedTagsLazy(ctx, sanitizedQuery)
		if len(semanticallyRelatedTagsMap) > 0 {
			type tagWeightDTO struct {
				Tag    string
				Weight float64
			}

			var relatedTagsList []tagWeightDTO
			for tag, weight := range semanticallyRelatedTagsMap {
				relatedTagsList = append(relatedTagsList, tagWeightDTO{Tag: tag, Weight: weight})
			}

			sort.Slice(relatedTagsList, func(i, j int) bool {
				return relatedTagsList[i].Weight > relatedTagsList[j].Weight
			})

			for _, relatedItem := range relatedTagsList {
				if int64(len(finalTagResults)) >= input.Limit {
					break
				}
				if !alreadySeenTagsMap[relatedItem.Tag] {
					finalTagResults = append(finalTagResults, relatedItem.Tag)
					alreadySeenTagsMap[relatedItem.Tag] = true
				}
			}
		}
	}

	// ── ÉTAPE 3 : REMPLISSAGE FALLBACK (ALPHABÉTIQUE) ───────────────────────
	for _, tag := range lexicographicTags {
		if int64(len(finalTagResults)) >= input.Limit {
			break
		}
		if !alreadySeenTagsMap[tag] {
			finalTagResults = append(finalTagResults, tag)
			alreadySeenTagsMap[tag] = true
		}
	}

	// ── ÉTAPE 4 : ASSEMBLAGE DU JSON FINAL ──────────────────────────────────
	if finalTagResults == nil {
		finalTagResults = make([]string, 0)
	}

	return search_models.AutocompleteTagOutput{
		Tags: finalTagResults,
	}, nil
}
