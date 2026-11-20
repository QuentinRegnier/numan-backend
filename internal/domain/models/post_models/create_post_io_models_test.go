package post_models_test

import (
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreatePostInput_Validate(t *testing.T) {
	assertBadRequestInvalidPayload := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	valid := func() post_models.CreatePostInput {
		return post_models.CreatePostInput{
			Content:     strings.Repeat("é", 2200),
			Hashtags:    []string{"paris", "voyage"},
			Identifiers: []int64{10, 20},
			MediaIDs:    []int64{101},
			IAScores:    []float64{0.1, 0.9},
			Location:    strings.Repeat("a", 100),
			Visibility:  variables.PostVisibilityAll,
		}
	}

	t.Run("nil receiver fails", func(t *testing.T) {
		var p *post_models.CreatePostInput
		assertBadRequestInvalidPayload(t, p.Validate())
	})

	t.Run("valid 2200 multibyte runes passes", func(t *testing.T) {
		p := valid()
		require.NoError(t, p.Validate())
	})

	cases := []struct {
		name   string
		mutate func(p *post_models.CreatePostInput)
	}{
		{"content > 2200 runes", func(p *post_models.CreatePostInput) { p.Content = strings.Repeat("é", 2201) }},
		{"location > 100 runes", func(p *post_models.CreatePostInput) { p.Location = strings.Repeat("é", 101) }},
		{"empty media_ids", func(p *post_models.CreatePostInput) { p.MediaIDs = nil }},
		{"non-positive media_id", func(p *post_models.CreatePostInput) { p.MediaIDs = []int64{0} }},
		{"too many unique media_ids", func(p *post_models.CreatePostInput) { p.MediaIDs = []int64{1, 2, 3, 4, 5} }},
		{"blank hashtag", func(p *post_models.CreatePostInput) { p.Hashtags = []string{"  "} }},
		{"hashtag > 20 runes", func(p *post_models.CreatePostInput) { p.Hashtags = []string{strings.Repeat("a", 21)} }},
		{"too many unique hashtags", func(p *post_models.CreatePostInput) {
			p.Hashtags = make([]string, variables.MaxHashtagsPerPost+1)
			for i := range p.Hashtags {
				p.Hashtags[i] = strings.Repeat("h", 1) + string(rune('a'+i))
			}
		}},
		{"non-positive identifier", func(p *post_models.CreatePostInput) { p.Identifiers = []int64{-1} }},
		{"too many unique identifiers", func(p *post_models.CreatePostInput) {
			p.Identifiers = []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
		}},
		{"too many ia scores", func(p *post_models.CreatePostInput) { p.IAScores = []float64{0, 0, 0, 0, 0} }},
		{"ia score out of bounds", func(p *post_models.CreatePostInput) { p.IAScores = []float64{1.2} }},
		{"ia score NaN", func(p *post_models.CreatePostInput) { p.IAScores = []float64{math.NaN()} }},
		{"visibility quarantined rejected on create", func(p *post_models.CreatePostInput) { p.Visibility = variables.PostVisibilityQuarantine }},
		{"visibility negative rejected", func(p *post_models.CreatePostInput) { p.Visibility = -1 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := valid()
			tc.mutate(&p)
			assertBadRequestInvalidPayload(t, p.Validate())
		})
	}
}
