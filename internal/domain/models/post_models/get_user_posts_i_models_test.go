package post_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserPostsInput_Validate(t *testing.T) {
	assertBadRequestInvalidPayload := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

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
}
