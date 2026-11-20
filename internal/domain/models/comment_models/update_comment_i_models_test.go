package comment_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/comment_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateCommentInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *comment_models.UpdateCommentInput
	assertBadRequest(t, nilIn.Validate())
	assertBadRequest(t, (&comment_models.UpdateCommentInput{CommentID: 0, Content: "ok"}).Validate())
	assertBadRequest(t, (&comment_models.UpdateCommentInput{CommentID: 1, Content: ""}).Validate())
	assertBadRequest(t, (&comment_models.UpdateCommentInput{CommentID: 1, Content: strings.Repeat("é", 2201)}).Validate())
	require.NoError(t, (&comment_models.UpdateCommentInput{CommentID: 1, Content: "Modifié"}).Validate())
}
