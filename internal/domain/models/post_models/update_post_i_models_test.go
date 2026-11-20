package post_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdatePostInput_Validate(t *testing.T) {
	assertBadRequestInvalidPayload := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	valid := func() post_models.UpdatePostInput {
		return post_models.UpdatePostInput{
			PostID:      123,
			Content:     "Contenu mis à jour",
			Hashtags:    []string{"tag1"},
			Identifiers: []int64{456},
			Location:    "Lyon",
			Visibility:  variables.PostVisibilityFriend,
		}
	}

	t.Run("nil receiver fails", func(t *testing.T) {
		var p *post_models.UpdatePostInput
		assertBadRequestInvalidPayload(t, p.Validate())
	})

	t.Run("valid input passes", func(t *testing.T) {
		p := valid()
		require.NoError(t, p.Validate())
	})

	cases := []struct {
		name   string
		mutate func(p *post_models.UpdatePostInput)
	}{
		{"invalid post_id", func(p *post_models.UpdatePostInput) { p.PostID = 0 }},
		{"content > 2200", func(p *post_models.UpdatePostInput) { p.Content = strings.Repeat("a", 2201) }},
		{"location > 100", func(p *post_models.UpdatePostInput) { p.Location = strings.Repeat("a", 101) }},
		{"blank hashtag", func(p *post_models.UpdatePostInput) { p.Hashtags = []string{""} }},
		{"hashtag > 50", func(p *post_models.UpdatePostInput) { p.Hashtags = []string{strings.Repeat("a", 51)} }},
		{"too many hashtags", func(p *post_models.UpdatePostInput) {
			p.Hashtags = make([]string, variables.MaxHashtagsPerPost+1)
			for i := range p.Hashtags {
				p.Hashtags[i] = strings.Repeat("h", 1) + string(rune('a'+i))
			}
		}},
		{"non-positive identifier", func(p *post_models.UpdatePostInput) { p.Identifiers = []int64{0} }},
		{"too many identifiers", func(p *post_models.UpdatePostInput) {
			p.Identifiers = []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
		}},
		{"unsupported visibility", func(p *post_models.UpdatePostInput) { p.Visibility = 3 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := valid()
			tc.mutate(&p)
			assertBadRequestInvalidPayload(t, p.Validate())
		})
	}
}
