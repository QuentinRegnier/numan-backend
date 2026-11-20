package article_service

import (
	"context"
	"fmt"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// GetArticleService récupère le détail d'un article avec filtrage de visibilité selon le rôle (utilisateur vs admin).
// - Utilisateur standard : uniquement les articles publiés (visibility == 0) et non expirés.
// - Administrateur : articles publiés, brouillons (visibility == 1) et expirés.
// - Personne : les articles supprimés logiquement (visibility == -1) retournent toujours 404.
func GetArticleService(ctx context.Context, callerID int64, isAdmin bool, input article_models.GetArticleInput) (article_models.GetArticleOutput, error) {
	if err := input.Validate(); err != nil {
		return article_models.GetArticleOutput{}, err
	}

	articleID := input.ResolvedID()
	article, err := object_cache_service.GetArticleFromCache(ctx, articleID)
	if err != nil {
		return article_models.GetArticleOutput{}, err
	}

	if article.Visibility < 0 {
		return article_models.GetArticleOutput{}, numan_error.NewNotFound(
			numan_error.CodeNotFound,
			"Article introuvable.",
			fmt.Errorf("article %d is soft-deleted", articleID),
		)
	}

	if !isAdmin {
		nowMs := domain.NowMillis()
		if article.Visibility != variables.ArticleVisibilityPublished || article.IsExpired(nowMs) {
			return article_models.GetArticleOutput{}, numan_error.NewNotFound(
				numan_error.CodeNotFound,
				"Article introuvable.",
				fmt.Errorf("article %d is not accessible to non-admin caller %d (visibility=%d)", articleID, callerID, article.Visibility),
			)
		}
	}

	return article_models.GetArticleOutput{
		Article: BuildArticleDetailView(ctx, article),
	}, nil
}
