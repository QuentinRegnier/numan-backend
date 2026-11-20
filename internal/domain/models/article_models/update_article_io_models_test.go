package article_models_test

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateArticleInput_ValidateAndNormalize(t *testing.T) {
	title := "  Titre Modifié  "
	summary := "  Nouveau résumé  "
	vis := variables.ArticleVisibilityDraft
	atts := []int64{50, 60, 50}
	coverID := int64(0)
	mdID := int64(25)
	partnerURL := "  HTTPS://partner.numan.app/promo  "
	exp := int64(1800000000000)
	expectedUpdatedAt := int64(1700000000000)

	in := article_models.UpdateArticleInput{
		ArticleID:          123,
		Title:              &title,
		Summary:            &summary,
		CoverMediaID:       &coverID,
		MarkdownMediaID:    &mdID,
		AttachmentMediaIDs: &atts,
		PartnerURL:         &partnerURL,
		Visibility:         &vis,
		ExpiresAt:          &exp,
		ExpectedUpdatedAt:  &expectedUpdatedAt,
	}
	require.NoError(t, in.ValidateAndNormalize())
	assert.Equal(t, int64(123), in.ResolvedID())
	assert.Equal(t, "Titre Modifié", *in.Title)
	assert.Equal(t, "Nouveau résumé", *in.Summary)
	assert.Equal(t, []int64{50, 60}, *in.AttachmentMediaIDs)
	assert.Equal(t, "https://partner.numan.app/promo", *in.PartnerURL)

	// Alias ID resolution
	aliasIn := article_models.UpdateArticleInput{ID: 456}
	require.NoError(t, aliasIn.ValidateAndNormalize())
	assert.Equal(t, int64(456), aliasIn.ResolvedID())

	// Missing ID
	noID := article_models.UpdateArticleInput{}
	assert.Error(t, noID.ValidateAndNormalize())

	// Empty Title
	emptyTitle := "   "
	badTitle := article_models.UpdateArticleInput{ArticleID: 123, Title: &emptyTitle}
	assert.Error(t, badTitle.ValidateAndNormalize())

	// Negative cover_media_id / non-positive markdown_media_id
	negCover := int64(-1)
	assert.Error(t, (&article_models.UpdateArticleInput{ArticleID: 123, CoverMediaID: &negCover}).ValidateAndNormalize())
	zeroMD := int64(0)
	assert.Error(t, (&article_models.UpdateArticleInput{ArticleID: 123, MarkdownMediaID: &zeroMD}).ValidateAndNormalize())

	// Invalid visibility / negative expires_at / invalid expected_updated_at
	badVis := variables.ArticleVisibilitySoftDeleted
	assert.Error(t, (&article_models.UpdateArticleInput{ArticleID: 123, Visibility: &badVis}).ValidateAndNormalize())
	negExp := int64(-5)
	assert.Error(t, (&article_models.UpdateArticleInput{ArticleID: 123, ExpiresAt: &negExp}).ValidateAndNormalize())
	zeroConc := int64(0)
	assert.Error(t, (&article_models.UpdateArticleInput{ArticleID: 123, ExpectedUpdatedAt: &zeroConc}).ValidateAndNormalize())
}
