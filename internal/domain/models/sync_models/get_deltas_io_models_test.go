package sync_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/sync_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDeltasInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *sync_models.GetDeltasInput
	assertBadRequest(t, nilIn.Validate())
	assertBadRequest(t, (&sync_models.GetDeltasInput{SinceMs: -1}).Validate())
	require.NoError(t, (&sync_models.GetDeltasInput{SinceMs: 0}).Validate())
	require.NoError(t, (&sync_models.GetDeltasInput{SinceMs: 1700000000000}).Validate())
}
