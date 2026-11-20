package article_models_test

import (
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateArticleInput_ValidateAndNormalize(t *testing.T) {
	now := int64(1700000000000)
	future := now + 3600000
	in := article_models.CreateArticleInput{
		Title:              "  Nouvel Article  ",
		Summary:            "  Résumé court  ",
		CoverMediaID:       10,
		MarkdownMediaID:    20,
		AttachmentMediaIDs: []int64{30, 40, 30},
		PartnerURL:         "https://example.org/promo",
		ExpiresAt:          &future,
	}
	require.NoError(t, in.ValidateAndNormalize(now))
	assert.Equal(t, "Nouvel Article", in.Title)
	assert.Equal(t, "Résumé court", in.Summary)
	assert.Equal(t, []int64{30, 40}, in.AttachmentMediaIDs)
	require.NotNil(t, in.Visibility)
	assert.Equal(t, variables.ArticleVisibilityPublished, *in.Visibility)

	// Negative ID
	negID := in
	negID.ID = -1
	assert.Error(t, negID.ValidateAndNormalize(now))

	// Empty or overly long Title
	emptyTitle := in
	emptyTitle.Title = "   "
	assert.Error(t, emptyTitle.ValidateAndNormalize(now))

	longTitle := in
	longTitle.Title = strings.Repeat("a", variables.MaxArticleTitleLength+1)
	assert.Error(t, longTitle.ValidateAndNormalize(now))

	// Overly long Summary
	longSummary := in
	longSummary.Summary = strings.Repeat("b", variables.MaxArticleSummaryLength+1)
	assert.Error(t, longSummary.ValidateAndNormalize(now))

	// Negative CoverMediaID
	negCover := in
	negCover.CoverMediaID = -1
	assert.Error(t, negCover.ValidateAndNormalize(now))

	// Missing MarkdownMediaID
	noMd := in
	noMd.MarkdownMediaID = 0
	assert.Error(t, noMd.ValidateAndNormalize(now))

	// Expired timestamp on creation
	past := now - 1000
	expiredIn := in
	expiredIn.ExpiresAt = &past
	assert.Error(t, expiredIn.ValidateAndNormalize(now))

	// Non-positive expires_at normalizes to nil
	zeroExp := int64(0)
	clearExp := in
	clearExp.ExpiresAt = &zeroExp
	require.NoError(t, clearExp.ValidateAndNormalize(now))
	assert.Nil(t, clearExp.ExpiresAt)

	// Soft-deleted visibility forbidden on creation
	delVis := variables.ArticleVisibilitySoftDeleted
	badVis := in
	badVis.Visibility = &delVis
	assert.Error(t, badVis.ValidateAndNormalize(now))
}
