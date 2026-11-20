package comment_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/comment_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCommentsInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *comment_models.GetCommentsInput
	assertBadRequest(t, nilIn.Validate())
	assertBadRequest(t, (&comment_models.GetCommentsInput{PostID: 0}).Validate())
	assertBadRequest(t, (&comment_models.GetCommentsInput{PostID: 1, Offset: -1}).Validate())
	assertBadRequest(t, (&comment_models.GetCommentsInput{PostID: 1, Limit: -1}).Validate())
	assertBadRequest(t, (&comment_models.GetCommentsInput{PostID: 1, Limit: 101}).Validate())
	badAfter := int64(0)
	assertBadRequest(t, (&comment_models.GetCommentsInput{PostID: 1, AfterID: &badAfter}).Validate())
	goodAfter := int64(42)
	require.NoError(t, (&comment_models.GetCommentsInput{PostID: 1, Limit: 50, Offset: 0, AfterID: &goodAfter}).Validate())
}
