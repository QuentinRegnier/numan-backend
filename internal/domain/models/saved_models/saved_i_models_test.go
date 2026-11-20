package saved_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/saved_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveActionInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *saved_models.SaveActionInput
	assertBadRequest(t, nilIn.Validate())
	assertBadRequest(t, (&saved_models.SaveActionInput{PostID: 0}).Validate())
	assertBadRequest(t, (&saved_models.SaveActionInput{PostID: -5}).Validate())
	require.NoError(t, (&saved_models.SaveActionInput{PostID: 42}).Validate())
}
