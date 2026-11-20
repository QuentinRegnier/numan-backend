package article_service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListArticlesService_RoleFilteringAndLightweightPayload(t *testing.T) {
	ctx := context.Background()
	_ = test.SetupTestRedis(t)
	t.Setenv("PUBLIC_MEDIA_BASE_URL", "https://cdn.numan.app/public")

	now := domain.NowMillis()
	pastExp := now - 60000

	for _, m := range []media_models.MediaPayload{
		{ID: 2001, OwnerID: 9001, StoragePath: "articles/cover-1.avif", Visibility: variables.MediaVisibilityPublic, CreatedAt: now},
		{ID: 3001, OwnerID: 9001, StoragePath: "articles/doc-1.md", Visibility: variables.MediaVisibilityPublic, CreatedAt: now},
	} {
		require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, m))
	}

	for _, art := range []article_models.ArticlePayload{
		{ID: 5001, Title: "Guide de Lyon", CoverMediaID: 2001, MarkdownMediaID: 3001, Visibility: variables.ArticleVisibilityPublished, CreatedAt: now, UpdatedAt: now},
		{ID: 5002, Title: "Brouillon Secret", MarkdownMediaID: 3001, Visibility: variables.ArticleVisibilityDraft, CreatedAt: now + 10, UpdatedAt: now + 10},
		{ID: 5003, Title: "Offre Expirée", MarkdownMediaID: 3001, Visibility: variables.ArticleVisibilityPublished, ExpiresAt: &pastExp, CreatedAt: now + 20, UpdatedAt: now + 20},
	} {
		require.NoError(t, object_cache_service.SetArticleInCache(ctx, art))
	}

	userList, err := ListArticlesService(ctx, 1001, false, article_models.GetArticlesInput{Limit: 10, Offset: 0})
	require.NoError(t, err)
	assert.Equal(t, 1, userList.Total)
	require.Len(t, userList.Articles, 1)
	assert.Equal(t, int64(5001), userList.Articles[0].ID)
	assert.Equal(t, "https://cdn.numan.app/public/articles/cover-1.avif", userList.Articles[0].CoverURL)

	// La réponse liste ne doit jamais contenir markdown_url ni attachments et doit rester < 2 Ko
	listJSON, err := json.Marshal(userList)
	require.NoError(t, err)
	assert.NotContains(t, string(listJSON), "markdown_url")
	assert.NotContains(t, string(listJSON), "attachments")
	assert.Less(t, len(listJSON), 2048)

	// Admin voit publiés, brouillons et expirés + pagination fonctionnelle
	adminList, err := ListArticlesService(ctx, 9001, true, article_models.GetArticlesInput{Limit: 2, Offset: 1})
	require.NoError(t, err)
	assert.Equal(t, 3, adminList.Total)
	require.Len(t, adminList.Articles, 2)
	assert.Equal(t, int64(5002), adminList.Articles[0].ID)
	assert.Equal(t, int64(5001), adminList.Articles[1].ID)

	// Paramètres négatifs rejetés
	_, errInvalid := ListArticlesService(ctx, 1001, false, article_models.GetArticlesInput{Limit: -1})
	assert.Error(t, errInvalid)
}
