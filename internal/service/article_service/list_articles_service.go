package article_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ListArticlesService retourne la liste paginée et légère des articles depuis l'index Redis L1 persistant.
// - Utilisateur standard : uniquement les articles publiés (visibility == 0) et non expirés.
// - Administrateur : articles publiés, brouillons (visibility == 1) et expirés.
// - Personne : les articles supprimés logiquement (visibility == -1) sont toujours exclus.
// Hydrate uniquement les URLs de couverture de la tranche paginée en un seul appel MGET batch.
func ListArticlesService(ctx context.Context, callerID int64, isAdmin bool, input article_models.GetArticlesInput) (article_models.GetArticlesOutput, error) {
	if err := input.ValidateAndNormalize(); err != nil {
		return article_models.GetArticlesOutput{}, err
	}

	allActive, err := object_cache_service.ListArticlesFromCache(ctx)
	if err != nil {
		return article_models.GetArticlesOutput{}, err
	}

	nowMs := domain.NowMillis()
	filtered := make([]article_models.ArticlePayload, 0, len(allActive))
	for _, art := range allActive {
		if art.Visibility < 0 {
			continue
		}
		if !isAdmin {
			if art.Visibility != variables.ArticleVisibilityPublished || art.IsExpired(nowMs) {
				continue
			}
		}
		filtered = append(filtered, art)
	}

	total := len(filtered)
	var pageSlice []article_models.ArticlePayload
	if input.Offset < total {
		end := input.Offset + input.Limit
		if end > total {
			end = total
		}
		pageSlice = filtered[input.Offset:end]
	}

	items := BuildArticleListItemViews(ctx, pageSlice)

	return article_models.GetArticlesOutput{
		Articles: items,
		Total:    total,
		Limit:    input.Limit,
		Offset:   input.Offset,
	}, nil
}
