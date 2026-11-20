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
)

// UpdateArticleService met à jour un article existant avec contrôle de concurrence optimiste,
// activation des nouveaux médias, calcul différentiel serveur des médias détachés et protection des médias partagés.
func UpdateArticleService(ctx context.Context, adminID int64, input article_models.UpdateArticleInput) (article_models.UpdateArticleOutput, error) {
	if err := input.ValidateAndNormalize(); err != nil {
		return article_models.UpdateArticleOutput{}, err
	}

	articleID := input.ResolvedID()
	existing, errGet := object_cache_service.GetArticleFromCache(ctx, articleID)
	if errGet != nil {
		return article_models.UpdateArticleOutput{}, errGet
	}

	// Contrôle de concurrence optimiste (si expected_updated_at est fourni par le client)
	if input.ExpectedUpdatedAt != nil && *input.ExpectedUpdatedAt != existing.UpdatedAt {
		return article_models.UpdateArticleOutput{}, numan_error.NewConflict(
			numan_error.CodeConflict,
			"L'article a été modifié concurremment. Veuillez recharger la dernière version.",
			fmt.Errorf("optimistic concurrency conflict on article %d: expected updated_at=%d, actual=%d", articleID, *input.ExpectedUpdatedAt, existing.UpdatedAt),
		)
	}

	oldReferencedMedia := existing.ReferencedMediaIDs()
	updated := existing

	if input.Title != nil {
		updated.Title = *input.Title
	}
	if input.Summary != nil {
		updated.Summary = *input.Summary
	}
	if input.CoverMediaID != nil {
		updated.CoverMediaID = *input.CoverMediaID
	}
	if input.MarkdownMediaID != nil {
		updated.MarkdownMediaID = *input.MarkdownMediaID
	}
	if input.AttachmentMediaIDs != nil {
		updated.AttachmentMediaIDs = *input.AttachmentMediaIDs
	}
	if input.PartnerURL != nil {
		updated.PartnerURL = *input.PartnerURL
	}
	if input.Visibility != nil {
		updated.Visibility = *input.Visibility
	}
	if input.ClearExpiresAt {
		updated.ExpiresAt = nil
	} else if input.ExpiresAt != nil {
		if *input.ExpiresAt <= 0 {
			updated.ExpiresAt = nil
		} else {
			expVal := *input.ExpiresAt
			updated.ExpiresAt = &expVal
		}
	}

	// Horodatage strictement monotone pour garantir l'ordre des événements Write-Behind
	now := domain.NowMillis()
	if now <= existing.UpdatedAt {
		now = existing.UpdatedAt + 1
	}
	updated.UpdatedAt = now

	if errVal := updated.ValidateAndNormalize(); errVal != nil {
		return article_models.UpdateArticleOutput{}, errVal
	}

	// Validation sémantique et activation des médias référencés par la nouvelle version
	if errMedia := media_service.ValidateAndActivateAdminArticleMedia(
		ctx,
		updated.CoverMediaID,
		updated.MarkdownMediaID,
		updated.AttachmentMediaIDs,
	); errMedia != nil {
		return article_models.UpdateArticleOutput{}, errMedia
	}

	removedMediaIDs := diffRemovedMediaIDs(oldReferencedMedia, updated.ReferencedMediaIDs())

	// 1. Mise à jour de l'index Redis L1
	if errCache := object_cache_service.SetArticleInCache(ctx, updated); errCache != nil {
		return article_models.UpdateArticleOutput{}, errCache
	}

	// 2. Enqueue Write-Behind vers MongoDB L2 et PostgreSQL L3 (avec restauration L1 si échec)
	if errQ := redis.EnqueueDB(ctx, updated.ID, updated.ID, redis.EntityArticle, redis.ActionUpdate, updated, redis.TargetAll); errQ != nil {
		_ = object_cache_service.SetArticleInCache(ctx, existing)
		numan_log.Error(ctx).Err(errQ).Int64("article_id", updated.ID).Msg("Échec EnqueueDB lors de la mise à jour d'article, restauration L1 effectuée")
		return article_models.UpdateArticleOutput{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de planifier la mise à jour de l'article.",
			errQ,
		)
	}

	// 3. Soft-delete différé des anciens médias qui ne sont plus référencés par aucun article actif (aucune suppression S3 synchrone)
	var deactivatedIDs []int64
	if len(removedMediaIDs) > 0 {
		var errDeact error
		deactivatedIDs, errDeact = media_service.DeactivateAdminMediaBatch(ctx, removedMediaIDs, updated.ID)
		if errDeact != nil {
			numan_log.Warn(ctx).Err(errDeact).Int64("article_id", updated.ID).Msg("Avertissement lors de la désactivation des anciens médias d'article")
		}
	}

	numan_log.Info(ctx).
		Int64("admin_id", adminID).
		Int64("article_id", updated.ID).
		Int("visibility", updated.Visibility).
		Int("deactivated_media_count", len(deactivatedIDs)).
		Msg("Article Markdown mis à jour avec succès")

	return article_models.UpdateArticleOutput{
		Article:            BuildArticleDetailView(ctx, updated),
		DeactivatedMediaID: deactivatedIDs,
	}, nil
}
