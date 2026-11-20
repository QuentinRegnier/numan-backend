package article_service

import (
	"context"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// CreateArticleService crée un nouvel article Markdown, valide et active ses médias publics,
// met à jour l'index Redis L1 persistant et planifie la persistance Write-Behind L2/L3 avec compensation en cas d'échec.
func CreateArticleService(ctx context.Context, adminID int64, input article_models.CreateArticleInput) (article_models.CreateArticleOutput, error) {
	now := domain.NowMillis()
	if err := input.ValidateAndNormalize(now); err != nil {
		return article_models.CreateArticleOutput{}, err
	}

	articleID := input.ID
	if articleID <= 0 {
		articleID = pkg.GenerateID()
	}

	// Validation sémantique et activation des médias d'article (support cross-admin pour préfixe articles/)
	if errMedia := media_service.ValidateAndActivateAdminArticleMedia(
		ctx,
		input.CoverMediaID,
		input.MarkdownMediaID,
		input.AttachmentMediaIDs,
	); errMedia != nil {
		return article_models.CreateArticleOutput{}, errMedia
	}

	vis := variables.ArticleVisibilityPublished
	if input.Visibility != nil {
		vis = *input.Visibility
	}

	payload := article_models.ArticlePayload{
		ID:                 articleID,
		Title:              input.Title,
		Summary:            input.Summary,
		CoverMediaID:       input.CoverMediaID,
		MarkdownMediaID:    input.MarkdownMediaID,
		AttachmentMediaIDs: input.AttachmentMediaIDs,
		PartnerURL:         input.PartnerURL,
		Visibility:         vis,
		ExpiresAt:          input.ExpiresAt,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if errVal := payload.ValidateAndNormalize(); errVal != nil {
		return article_models.CreateArticleOutput{}, errVal
	}

	// 1. Écriture synchrone dans l'index Redis L1 persistant (TTL 0)
	if errCache := object_cache_service.SetArticleInCache(ctx, payload); errCache != nil {
		return article_models.CreateArticleOutput{}, errCache
	}

	// 2. Enqueue Write-Behind vers MongoDB L2 et PostgreSQL L3 (avec rollback L1 si la file échoue)
	if errQ := redis.EnqueueDB(ctx, payload.ID, payload.ID, redis.EntityArticle, redis.ActionCreate, payload, redis.TargetAll); errQ != nil {
		_ = object_cache_service.DeleteArticleFromCache(ctx, payload.ID)
		numan_log.Error(ctx).Err(errQ).Int64("article_id", payload.ID).Msg("Échec EnqueueDB lors de la création d'article, rollback L1 effectué")
		return article_models.CreateArticleOutput{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de planifier la sauvegarde de l'article.",
			errQ,
		)
	}

	numan_log.Info(ctx).
		Int64("admin_id", adminID).
		Int64("article_id", payload.ID).
		Int("visibility", payload.Visibility).
		Msg("Article Markdown créé avec succès")

	return article_models.CreateArticleOutput{
		Article: BuildArticleDetailView(ctx, payload),
	}, nil
}
