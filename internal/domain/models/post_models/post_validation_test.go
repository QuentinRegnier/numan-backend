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

func assertBadRequestInvalidPayload(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestPostPayload_Validate(t *testing.T) {
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

func TestCreatePostInput_Validate(t *testing.T) {
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

func TestUpdatePostInput_Validate(t *testing.T) {
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

func TestDeleteAndQueryPostInputs_Validate(t *testing.T) {
	t.Run("DeletePostInput", func(t *testing.T) {
		var nilDel *post_models.DeletePostInput
		assertBadRequestInvalidPayload(t, nilDel.Validate())
		assertBadRequestInvalidPayload(t, (&post_models.DeletePostInput{PostID: 0}).Validate())
		require.NoError(t, (&post_models.DeletePostInput{PostID: 1}).Validate())
	})

	t.Run("GetPostInput", func(t *testing.T) {
		var nilGet *post_models.GetPostInput
		assertBadRequestInvalidPayload(t, nilGet.Validate())
		assertBadRequestInvalidPayload(t, (&post_models.GetPostInput{}).Validate())
		assertBadRequestInvalidPayload(t, (&post_models.GetPostInput{PostIDs: []int64{1, 0}}).Validate())

		tooMany := make([]int64, 101)
		for i := range tooMany {
			tooMany[i] = int64(i + 1)
		}
		assertBadRequestInvalidPayload(t, (&post_models.GetPostInput{PostIDs: tooMany}).Validate())
		require.NoError(t, (&post_models.GetPostInput{PostIDs: []int64{1, 2}}).Validate())
	})

	t.Run("GetUserPostsInput", func(t *testing.T) {
		var nilU *post_models.GetUserPostsInput
		assertBadRequestInvalidPayload(t, nilU.Validate())
		assertBadRequestInvalidPayload(t, (&post_models.GetUserPostsInput{TargetUserID: 0}).Validate())
		assertBadRequestInvalidPayload(t, (&post_models.GetUserPostsInput{TargetUserID: 1, Offset: -1}).Validate())
		assertBadRequestInvalidPayload(t, (&post_models.GetUserPostsInput{TargetUserID: 1, Limit: -1}).Validate())
		assertBadRequestInvalidPayload(t, (&post_models.GetUserPostsInput{TargetUserID: 1, Limit: 101}).Validate())
		badBefore := int64(0)
		assertBadRequestInvalidPayload(t, (&post_models.GetUserPostsInput{TargetUserID: 1, BeforeID: &badBefore}).Validate())

		goodBefore := int64(55)
		require.NoError(t, (&post_models.GetUserPostsInput{TargetUserID: 1, Limit: 0, Offset: 0, BeforeID: &goodBefore}).Validate())
	})
}
