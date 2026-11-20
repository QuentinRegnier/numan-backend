package article_models_test

import (
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func TestArticlePayload_ValidateAndNormalize(t *testing.T) {
	exp := int64(2000000000000)
	valid := article_models.ArticlePayload{
		ID:                 1001,
		Title:              "  Guide de Paris  ",
		Summary:            "  Découvrez les meilleurs lieux  ",
		CoverMediaID:       2001,
		MarkdownMediaID:    3001,
		AttachmentMediaIDs: []int64{4001, 4002, 4001},
		PartnerURL:         "  HTTPS://partenaire.fr/offre?utm=numan  ",
		Visibility:         variables.ArticleVisibilityPublished,
		ExpiresAt:          &exp,
		CreatedAt:          1700000000000,
		UpdatedAt:          1700000000000,
	}

	require.NoError(t, valid.ValidateAndNormalize())
	assert.Equal(t, "Guide de Paris", valid.Title)
	assert.Equal(t, "Découvrez les meilleurs lieux", valid.Summary)
	assert.Equal(t, []int64{4001, 4002}, valid.AttachmentMediaIDs)
	assert.Equal(t, "https://partenaire.fr/offre?utm=numan", valid.PartnerURL)
	assert.Equal(t, []int64{2001, 3001, 4001, 4002}, valid.ReferencedMediaIDs())
	assert.False(t, valid.IsExpired(1700000000000))
	assert.True(t, valid.IsExpired(2000000000001))

	// MsgPack roundtrip
	packed, errPack := msgpack.Marshal(valid)
	require.NoError(t, errPack)
	var unpacked article_models.ArticlePayload
	require.NoError(t, msgpack.Unmarshal(packed, &unpacked))
	assert.Equal(t, valid, unpacked)

	// Invalid ID
	badID := valid
	badID.ID = 0
	assert.Error(t, badID.ValidateAndNormalize())

	// Empty Title
	badTitle := valid
	badTitle.Title = "   "
	assert.Error(t, badTitle.ValidateAndNormalize())

	// Title > 200 runes
	longTitle := valid
	longTitle.Title = strings.Repeat("é", 201)
	assert.Error(t, longTitle.ValidateAndNormalize())

	// Summary > 500 runes
	longSummary := valid
	longSummary.Summary = strings.Repeat("a", 501)
	assert.Error(t, longSummary.ValidateAndNormalize())

	// Negative cover/markdown IDs
	negCover := valid
	negCover.CoverMediaID = -1
	assert.Error(t, negCover.ValidateAndNormalize())

	negMd := valid
	negMd.MarkdownMediaID = -5
	assert.Error(t, negMd.ValidateAndNormalize())

	// Invalid visibility
	badVis := valid
	badVis.Visibility = 99
	assert.Error(t, badVis.ValidateAndNormalize())

	// Dangerous PartnerURL schemes
	for _, schemeURL := range []string{
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"file:///etc/passwd",
		"ftp://files.example.com/a",
		"https://",
		"http:///path",
	} {
		badURL := valid
		badURL.PartnerURL = schemeURL
		assert.Error(t, badURL.ValidateAndNormalize(), "expected error for partner_url=%s", schemeURL)
	}

	// Non-positive expires_at clears to nil
	zeroExp := int64(0)
	withZeroExp := valid
	withZeroExp.ExpiresAt = &zeroExp
	require.NoError(t, withZeroExp.ValidateAndNormalize())
	assert.Nil(t, withZeroExp.ExpiresAt)
}
