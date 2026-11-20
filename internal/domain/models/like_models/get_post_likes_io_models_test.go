package like_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/like_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPostLikesInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *like_models.GetPostLikesInput
	assertBadRequest(t, nilIn.Validate())
	assertBadRequest(t, (&like_models.GetPostLikesInput{CallerID: -1, PostID: 1}).Validate())
	assertBadRequest(t, (&like_models.GetPostLikesInput{CallerID: 1, PostID: 0}).Validate())
	assertBadRequest(t, (&like_models.GetPostLikesInput{CallerID: 1, PostID: 1, Offset: -1}).Validate())
	assertBadRequest(t, (&like_models.GetPostLikesInput{CallerID: 1, PostID: 1, Limit: -1}).Validate())
	assertBadRequest(t, (&like_models.GetPostLikesInput{CallerID: 1, PostID: 1, Limit: 101}).Validate())
	require.NoError(t, (&like_models.GetPostLikesInput{CallerID: 1, PostID: 1, Limit: 0, Offset: 0}).Validate())
	require.NoError(t, (&like_models.GetPostLikesInput{CallerID: 1, PostID: 1, Limit: 20, Offset: 10}).Validate())
}
