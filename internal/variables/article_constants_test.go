package variables_test

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
)

func TestArticleConstants(t *testing.T) {
	assert.Equal(t, -1, variables.ArticleVisibilitySoftDeleted)
	assert.Equal(t, 0, variables.ArticleVisibilityPublished)
	assert.Equal(t, 1, variables.ArticleVisibilityDraft)

	assert.Equal(t, "articles/", variables.ArticleMediaStoragePrefix)
	assert.Equal(t, "maps/", variables.MapsStoragePrefix)
	assert.Equal(t, "static/", variables.StaticStoragePrefix)
	assert.Equal(t, "numan-bucket", variables.DefaultPrivateBucketName)
	assert.Equal(t, "numan-public", variables.DefaultPublicBucketName)

	assert.Equal(t, 1024, variables.ArticleImageMaxDimStandard)
	assert.Equal(t, 2048, variables.ArticleImageMaxDimHigh)
	assert.Equal(t, "image", variables.ArticleAssetKindImage)
	assert.Equal(t, "markdown", variables.ArticleAssetKindMarkdown)

	assert.Equal(t, "articles:index:data", variables.RedisKeyArticlesIndexData)
	assert.Equal(t, "articles:index:order", variables.RedisKeyArticlesIndexOrder)

	assert.Equal(t, 200, variables.MaxArticleTitleLength)
	assert.Equal(t, 500, variables.MaxArticleSummaryLength)
	assert.Equal(t, 50, variables.MaxArticleAttachments)
	assert.Equal(t, 512, variables.MaxArticlePartnerURLLength)
	assert.Equal(t, 20, variables.DefaultArticleListLimit)
	assert.Equal(t, 100, variables.MaxArticleListLimit)

	assert.Equal(t, int64(15*1024*1024), variables.MaxArticleStaticImageBytes)
	assert.Equal(t, int64(5*1024*1024), variables.MaxArticleAnimatedGIFBytes)
	assert.Equal(t, int64(5*1024*1024), variables.MaxArticleAnimatedWebPBytes)
	assert.Equal(t, int64(256*1024), variables.MaxArticleMarkdownBytes)
	assert.Greater(t, variables.ArticleMaxImagePixels, 0)
}
