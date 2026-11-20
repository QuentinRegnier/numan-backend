package post_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeletePostInput_Validate(t *testing.T) {
	assertBadRequestInvalidPayload := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilDel *post_models.DeletePostInput
	assertBadRequestInvalidPayload(t, nilDel.Validate())
	assertBadRequestInvalidPayload(t, (&post_models.DeletePostInput{PostID: 0}).Validate())
	require.NoError(t, (&post_models.DeletePostInput{PostID: 1}).Validate())
}
