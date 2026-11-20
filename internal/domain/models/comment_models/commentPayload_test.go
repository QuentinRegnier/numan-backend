package comment_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/comment_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommentPayload_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	valid := func() comment_models.CommentPayload {
		return comment_models.CommentPayload{
			ID:         1,
			PostID:     2,
			UserID:     3,
			Content:    "Super publication !",
			Visibility: 0,
			LikeCount:  0,
		}
	}

	var nilP *comment_models.CommentPayload
	assertBadRequest(t, nilP.Validate())

	p := valid()
	require.NoError(t, p.Validate())
	p.Visibility = variables.CommentVisibilitySoftDelete
	require.NoError(t, p.Validate())

	cases := []struct {
		name   string
		mutate func(p *comment_models.CommentPayload)
	}{
		{"invalid ID", func(p *comment_models.CommentPayload) { p.ID = 0 }},
		{"invalid PostID", func(p *comment_models.CommentPayload) { p.PostID = -1 }},
		{"invalid UserID", func(p *comment_models.CommentPayload) { p.UserID = 0 }},
		{"blank content", func(p *comment_models.CommentPayload) { p.Content = "   " }},
		{"invalid visibility", func(p *comment_models.CommentPayload) { p.Visibility = 2 }},
		{"negative like count", func(p *comment_models.CommentPayload) { p.LikeCount = -1 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := valid()
			tc.mutate(&c)
			assertBadRequest(t, c.Validate())
		})
	}
}
