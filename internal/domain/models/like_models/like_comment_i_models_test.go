package like_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/like_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLikeCommentInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *like_models.LikeCommentInput
	assertBadRequest(t, nilIn.Validate())
	assertBadRequest(t, (&like_models.LikeCommentInput{CommentID: -5, Action: "like"}).Validate())
	assertBadRequest(t, (&like_models.LikeCommentInput{CommentID: 1, Action: ""}).Validate())
	require.NoError(t, (&like_models.LikeCommentInput{CommentID: 1, Action: "like"}).Validate())
	require.NoError(t, (&like_models.LikeCommentInput{CommentID: 1, Action: "unlike"}).Validate())
}
