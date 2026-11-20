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

func TestBuildArticleDetailView(t *testing.T) {
	ctx := context.Background()
	_ = test.SetupTestRedis(t)
	t.Setenv("PUBLIC_MEDIA_BASE_URL", "https://cdn.numan.app/public")

	now := domain.NowMillis()
	for _, m := range []media_models.MediaPayload{
		{ID: 101, OwnerID: 9001, StoragePath: "articles/cover.avif", Visibility: variables.MediaVisibilityPublic, CreatedAt: now},
		{ID: 102, OwnerID: 9001, StoragePath: "articles/guide.md", Visibility: variables.MediaVisibilityPublic, CreatedAt: now},
		{ID: 103, OwnerID: 9001, StoragePath: "articles/anim.gif", Visibility: variables.MediaVisibilityPublic, CreatedAt: now},
	} {
		require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, m))
	}

	zeroExp := int64(0)
	view := BuildArticleDetailView(ctx, article_models.ArticlePayload{
		ID:                 501,
		Title:              "Guide Complet",
		Summary:            "Résumé",
		CoverMediaID:       101,
		MarkdownMediaID:    102,
		AttachmentMediaIDs: []int64{103, 0, -1},
		PartnerURL:         "https://numan.app/partner",
		Visibility:         variables.ArticleVisibilityPublished,
		ExpiresAt:          &zeroExp,
		CreatedAt:          now,
		UpdatedAt:          now,
	})

	assert.Equal(t, int64(501), view.ID)
	assert.Equal(t, "Guide Complet", view.Title)
	assert.Nil(t, view.ExpiresAt)
	assert.Equal(t, "https://cdn.numan.app/public/articles/cover.avif", view.CoverURL)
	assert.Equal(t, "https://cdn.numan.app/public/articles/guide.md", view.MarkdownURL)
	require.Len(t, view.Attachments, 1)
	assert.Equal(t, "https://cdn.numan.app/public/articles/anim.gif", view.Attachments[0].URL)
}

func TestBuildArticleListItemViews(t *testing.T) {
	ctx := context.Background()
	_ = test.SetupTestRedis(t)
	t.Setenv("PUBLIC_MEDIA_BASE_URL", "https://cdn.numan.app/public")

	assert.Empty(t, BuildArticleListItemViews(ctx, nil))

	now := domain.NowMillis()
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, media_models.MediaPayload{
		ID:          201,
		OwnerID:     9001,
		StoragePath: "articles/list-cover.avif",
		Visibility:  variables.MediaVisibilityPublic,
		CreatedAt:   now,
	}))

	items := BuildArticleListItemViews(ctx, []article_models.ArticlePayload{
		{
			ID:                 601,
			Title:              "Article Léger",
			Summary:            "Résumé léger",
			CoverMediaID:       201,
			MarkdownMediaID:    202,
			AttachmentMediaIDs: []int64{203},
			Visibility:         variables.ArticleVisibilityPublished,
			CreatedAt:          now,
			UpdatedAt:          now,
		},
	})

	require.Len(t, items, 1)
	assert.Equal(t, "https://cdn.numan.app/public/articles/list-cover.avif", items[0].CoverURL)

	raw, err := json.Marshal(items)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "markdown_url")
	assert.NotContains(t, string(raw), "attachments")
	assert.Less(t, len(raw), 2048)
}

func TestDiffRemovedMediaIDs(t *testing.T) {
	assert.Nil(t, diffRemovedMediaIDs(nil, []int64{1, 2}))
	assert.Equal(t, []int64{10, 30}, diffRemovedMediaIDs([]int64{10, 20, 10, 0, -1, 30}, []int64{20, 40}))
	assert.Empty(t, diffRemovedMediaIDs([]int64{10, 20}, []int64{10, 20, 30}))
}
