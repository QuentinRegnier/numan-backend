package like_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/like_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestLikeModels_Validate(t *testing.T) {
	t.Run("LikePayload", func(t *testing.T) {
		var nilP *like_models.LikePayload
		assertBadRequest(t, nilP.Validate())

		valid := like_models.LikePayload{
			ID:         1,
			TargetType: variables.LikeTargetTypePost,
			TargetID:   10,
			UserID:     20,
		}
		require.NoError(t, valid.Validate())
		valid.TargetType = variables.LikeTargetTypeComment
		require.NoError(t, valid.Validate())

		bad := valid
		bad.ID = 0
		assertBadRequest(t, bad.Validate())

		bad = valid
		bad.TargetType = 99
		assertBadRequest(t, bad.Validate())

		bad = valid
		bad.TargetID = -1
		assertBadRequest(t, bad.Validate())

		bad = valid
		bad.UserID = 0
		assertBadRequest(t, bad.Validate())
	})

	t.Run("LikePostInput", func(t *testing.T) {
		var nilIn *like_models.LikePostInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&like_models.LikePostInput{PostID: 0, Action: "like"}).Validate())
		assertBadRequest(t, (&like_models.LikePostInput{PostID: 1, Action: "invalid"}).Validate())
		require.NoError(t, (&like_models.LikePostInput{PostID: 1, Action: "like"}).Validate())
		require.NoError(t, (&like_models.LikePostInput{PostID: 1, Action: "unlike"}).Validate())
	})

	t.Run("LikeCommentInput", func(t *testing.T) {
		var nilIn *like_models.LikeCommentInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&like_models.LikeCommentInput{CommentID: -5, Action: "like"}).Validate())
		assertBadRequest(t, (&like_models.LikeCommentInput{CommentID: 1, Action: ""}).Validate())
		require.NoError(t, (&like_models.LikeCommentInput{CommentID: 1, Action: "like"}).Validate())
		require.NoError(t, (&like_models.LikeCommentInput{CommentID: 1, Action: "unlike"}).Validate())
	})

	t.Run("GetPostLikesInput", func(t *testing.T) {
		var nilIn *like_models.GetPostLikesInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&like_models.GetPostLikesInput{CallerID: -1, PostID: 1}).Validate())
		assertBadRequest(t, (&like_models.GetPostLikesInput{CallerID: 1, PostID: 0}).Validate())
		assertBadRequest(t, (&like_models.GetPostLikesInput{CallerID: 1, PostID: 1, Offset: -1}).Validate())
		assertBadRequest(t, (&like_models.GetPostLikesInput{CallerID: 1, PostID: 1, Limit: -1}).Validate())
		assertBadRequest(t, (&like_models.GetPostLikesInput{CallerID: 1, PostID: 1, Limit: 101}).Validate())
		require.NoError(t, (&like_models.GetPostLikesInput{CallerID: 1, PostID: 1, Limit: 0, Offset: 0}).Validate())
		require.NoError(t, (&like_models.GetPostLikesInput{CallerID: 1, PostID: 1, Limit: 20, Offset: 10}).Validate())
	})
}
