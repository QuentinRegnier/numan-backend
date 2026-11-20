package article_models_test

import (
	"encoding/json"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArticleView_JSONSerialization(t *testing.T) {
	listItem := article_models.ArticleListItemView{
		ID:         101,
		Title:      "Titre",
		Summary:    "Résumé",
		CoverURL:   "https://cdn.numan.app/public/articles/cover.avif",
		Visibility: 0,
		CreatedAt:  1000,
		UpdatedAt:  1000,
	}
	rawList, err := json.Marshal(listItem)
	require.NoError(t, err)
	assert.NotContains(t, string(rawList), "markdown_url")
	assert.NotContains(t, string(rawList), "attachments")

	detail := article_models.ArticleDetailView{
		ID:              101,
		Title:           "Titre",
		Summary:         "Résumé",
		CoverMediaID:    201,
		CoverURL:        "https://cdn.numan.app/public/articles/cover.avif",
		MarkdownMediaID: 301,
		MarkdownURL:     "https://cdn.numan.app/public/articles/doc.md",
		Attachments: []article_models.ArticleAssetView{
			{MediaID: 401, StoragePath: "articles/img.avif", URL: "https://cdn.numan.app/public/articles/img.avif"},
		},
		Visibility: 0,
		CreatedAt:  1000,
		UpdatedAt:  1000,
	}
	rawDetail, err := json.Marshal(detail)
	require.NoError(t, err)
	assert.Contains(t, string(rawDetail), "markdown_url")
	assert.Contains(t, string(rawDetail), "attachments")
}
