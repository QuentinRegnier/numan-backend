package article_service

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetArticleService_VisibilityMatrix(t *testing.T) {
	ctx := context.Background()
	_ = test.SetupTestRedis(t)
	t.Setenv("PUBLIC_MEDIA_BASE_URL", "https://cdn.numan.app/public")

	now := domain.NowMillis()
	pastExp := now - 60000

	for _, m := range []media_models.MediaPayload{
		{ID: 3001, OwnerID: 9001, StoragePath: "articles/doc-1.md", Visibility: variables.MediaVisibilityPublic, CreatedAt: now},
		{ID: 3002, OwnerID: 9001, StoragePath: "articles/doc-draft.md", Visibility: variables.MediaVisibilityPublic, CreatedAt: now},
	} {
		require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, m))
	}

	for _, art := range []article_models.ArticlePayload{
		{ID: 5001, Title: "Guide de Lyon", MarkdownMediaID: 3001, Visibility: variables.ArticleVisibilityPublished, CreatedAt: now, UpdatedAt: now},
		{ID: 5002, Title: "Brouillon Secret", MarkdownMediaID: 3002, Visibility: variables.ArticleVisibilityDraft, CreatedAt: now, UpdatedAt: now},
		{ID: 5003, Title: "Offre Expirée", MarkdownMediaID: 3001, Visibility: variables.ArticleVisibilityPublished, ExpiresAt: &pastExp, CreatedAt: now, UpdatedAt: now},
		{ID: 5004, Title: "Supprimé", MarkdownMediaID: 3001, Visibility: variables.ArticleVisibilitySoftDeleted, CreatedAt: now, UpdatedAt: now},
	} {
		require.NoError(t, object_cache_service.SetArticleInCache(ctx, art))
	}

	regularUser := int64(1001)
	adminUser := int64(9001)

	// Utilisateur standard : voit publié non expiré
	userPub, err := GetArticleService(ctx, regularUser, false, article_models.GetArticleInput{ArticleID: 5001})
	require.NoError(t, err)
	assert.Equal(t, "Guide de Lyon", userPub.Article.Title)
	assert.Equal(t, "https://cdn.numan.app/public/articles/doc-1.md", userPub.Article.MarkdownURL)

	// Utilisateur standard : 404 sur brouillon, expiré et soft-deleted
	for _, hiddenID := range []int64{5002, 5003, 5004} {
		_, errHidden := GetArticleService(ctx, regularUser, false, article_models.GetArticleInput{ArticleID: hiddenID})
		require.Error(t, errHidden)
		var appErr *numan_error.AppError
		require.ErrorAs(t, errHidden, &appErr)
		assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
	}

	// Admin : accède aux brouillons et articles expirés, mais pas aux soft-deleted
	adminDraft, err := GetArticleService(ctx, adminUser, true, article_models.GetArticleInput{ArticleID: 5002})
	require.NoError(t, err)
	assert.Equal(t, "Brouillon Secret", adminDraft.Article.Title)

	adminExp, err := GetArticleService(ctx, adminUser, true, article_models.GetArticleInput{ArticleID: 5003})
	require.NoError(t, err)
	assert.Equal(t, "Offre Expirée", adminExp.Article.Title)

	_, errSoftDel := GetArticleService(ctx, adminUser, true, article_models.GetArticleInput{ArticleID: 5004})
	assert.Error(t, errSoftDel)

	// Validation input invalide
	_, errInvalid := GetArticleService(ctx, regularUser, false, article_models.GetArticleInput{ArticleID: 0})
	assert.Error(t, errInvalid)
}
