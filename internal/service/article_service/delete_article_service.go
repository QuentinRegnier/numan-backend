package article_service

import (
	"context"
	"fmt"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// DeleteArticleService supprime logiquement un ou plusieurs articles, les retire immédiatement de l'index actif Redis L1,
// planifie le soft-delete L2/L3 et marque les médias non partagés comme supprimés logiquement pour le garbage collector.
func DeleteArticleService(ctx context.Context, adminID int64, input article_models.DeleteArticleInput) (article_models.DeleteArticleOutput, error) {
	targetIDs, err := input.ResolvedIDs()
	if err != nil {
		return article_models.DeleteArticleOutput{}, err
	}
	if input.ExpectedUpdatedAt != nil && len(targetIDs) > 1 {
		return article_models.DeleteArticleOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Le champ expected_updated_at ne peut être utilisé que pour la suppression d'un seul article.",
			nil,
		)
	}

	deletedArticles := make([]article_models.ArticlePayload, 0, len(targetIDs))
	deletedIDs := make([]int64, 0, len(targetIDs))

	for _, id := range targetIDs {
		existing, errGet := object_cache_service.GetArticleFromCache(ctx, id)
		if errGet != nil {
			if len(targetIDs) == 1 {
				return article_models.DeleteArticleOutput{}, errGet
			}
			continue
		}

		if input.ExpectedUpdatedAt != nil && *input.ExpectedUpdatedAt != existing.UpdatedAt {
			return article_models.DeleteArticleOutput{}, numan_error.NewConflict(
				numan_error.CodeConflict,
				"L'article a été modifié concurremment. Veuillez recharger la dernière version.",
				fmt.Errorf("optimistic concurrency conflict on delete article %d: expected updated_at=%d, actual=%d", id, *input.ExpectedUpdatedAt, existing.UpdatedAt),
			)
		}

		// 1. Retrait immédiat de l'index actif L1
		if errDelL1 := object_cache_service.DeleteArticleFromCache(ctx, id); errDelL1 != nil {
			return article_models.DeleteArticleOutput{}, errDelL1
		}

		now := domain.NowMillis()
		if now <= existing.UpdatedAt {
			now = existing.UpdatedAt + 1
		}
		deletedPayload := existing
		deletedPayload.Visibility = variables.ArticleVisibilitySoftDeleted
		deletedPayload.UpdatedAt = now

		// 2. Enqueue Write-Behind (ActionDelete) avec restauration L1 en cas d'échec
		if errQ := redis.EnqueueDB(ctx, id, id, redis.EntityArticle, redis.ActionDelete, deletedPayload, redis.TargetAll); errQ != nil {
			_ = object_cache_service.SetArticleInCache(ctx, existing)
			numan_log.Error(ctx).Err(errQ).Int64("article_id", id).Msg("Échec EnqueueDB lors de la suppression d'article, restauration L1 effectuée")
			return article_models.DeleteArticleOutput{}, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeCacheError,
				"Impossible de planifier la suppression de l'article.",
				errQ,
			)
		}

		deletedArticles = append(deletedArticles, existing)
		deletedIDs = append(deletedIDs, id)
	}

	// 3. Désactivation différée des médias référencés par chaque article supprimé (avec protection des médias partagés)
	var allDeactivatedMedia []int64
	seenMedia := make(map[int64]struct{})
	for _, art := range deletedArticles {
		refMedia := art.ReferencedMediaIDs()
		if len(refMedia) > 0 {
			deact, errDeact := media_service.DeactivateAdminMediaBatch(ctx, refMedia, art.ID)
			if errDeact != nil {
				numan_log.Warn(ctx).Err(errDeact).Int64("article_id", art.ID).Msg("Avertissement lors de la désactivation des médias de l'article supprimé")
			}
			for _, mID := range deact {
				if _, ok := seenMedia[mID]; !ok {
					seenMedia[mID] = struct{}{}
					allDeactivatedMedia = append(allDeactivatedMedia, mID)
				}
			}
		}
	}

	numan_log.Info(ctx).
		Int64("admin_id", adminID).
		Int("deleted_count", len(deletedIDs)).
		Msg("Articles Markdown supprimés logiquement avec succès")

	return article_models.DeleteArticleOutput{
		DeletedCount:       len(deletedIDs),
		ArticleIDs:         deletedIDs,
		DeactivatedMediaID: allDeactivatedMedia,
	}, nil
}
