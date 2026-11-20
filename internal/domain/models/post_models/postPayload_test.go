package post_models_test

import (
	"math"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostPayload_Validate(t *testing.T) {
	assertBadRequestInvalidPayload := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	valid := func() post_models.PostPayload {
		return post_models.PostPayload{
			ID:           1001,
			UserID:       2001,
			Content:      "Hello world",
			Hashtags:     []string{"go", "numan", "user_2001"},
			Identifiers:  []int64{3001},
			MediaIDs:     []int64{4001},
			Visibility:   variables.PostVisibilityAll,
			IAScoreMax:   0.25,
			LikeCount:    0,
			CommentCount: 0,
			ViewCount:    0,
		}
	}

	t.Run("nil receiver fails", func(t *testing.T) {
		var p *post_models.PostPayload
		assertBadRequestInvalidPayload(t, p.Validate())
	})

	t.Run("valid payload with MaxHashtagsPerPost+1 (auto author tag)", func(t *testing.T) {
		p := valid()
		p.Hashtags = make([]string, variables.MaxHashtagsPerPost+1)
		for i := range p.Hashtags {
			p.Hashtags[i] = "tag"
		}
		require.NoError(t, p.Validate())
	})

	cases := []struct {
		name   string
		mutate func(p *post_models.PostPayload)
	}{
		{"invalid ID", func(p *post_models.PostPayload) { p.ID = 0 }},
		{"invalid UserID", func(p *post_models.PostPayload) { p.UserID = -1 }},
		{"invalid visibility", func(p *post_models.PostPayload) { p.Visibility = 99 }},
		{"negative like count", func(p *post_models.PostPayload) { p.LikeCount = -1 }},
		{"negative comment count", func(p *post_models.PostPayload) { p.CommentCount = -1 }},
		{"negative view count", func(p *post_models.PostPayload) { p.ViewCount = -1 }},
		{"negative report count", func(p *post_models.PostPayload) { p.ReportCount = -1 }},
		{"negative telemetry clicks", func(p *post_models.PostPayload) { p.TelemetryClicks = -1 }},
		{"ia score NaN", func(p *post_models.PostPayload) { p.IAScoreMax = math.NaN() }},
		{"ia score > 1", func(p *post_models.PostPayload) { p.IAScoreMax = 1.01 }},
		{"ia score < 0", func(p *post_models.PostPayload) { p.IAScoreMax = -0.01 }},
		{"too many hashtags", func(p *post_models.PostPayload) {
			p.Hashtags = make([]string, variables.MaxHashtagsPerPost+2)
		}},
		{"too many identifiers", func(p *post_models.PostPayload) {
			p.Identifiers = make([]int64, variables.MaxIdentifiersPerPost+1)
			for i := range p.Identifiers {
				p.Identifiers[i] = int64(i + 1)
			}
		}},
		{"non-positive identifier", func(p *post_models.PostPayload) { p.Identifiers = []int64{1, 0} }},
		{"too many media", func(p *post_models.PostPayload) {
			p.MediaIDs = []int64{1, 2, 3, 4, 5}
		}},
		{"non-positive media id", func(p *post_models.PostPayload) { p.MediaIDs = []int64{-5} }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := valid()
			tc.mutate(&p)
			assertBadRequestInvalidPayload(t, p.Validate())
		})
	}
}
