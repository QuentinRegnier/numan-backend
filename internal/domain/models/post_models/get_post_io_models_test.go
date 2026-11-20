package post_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPostInput_Validate(t *testing.T) {
	assertBadRequestInvalidPayload := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

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
}
