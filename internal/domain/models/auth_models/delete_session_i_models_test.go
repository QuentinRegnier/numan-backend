package auth_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteSessionInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilD *auth_models.DeleteSessionInput
	assertBadRequest(t, nilD.Validate())

	require.NoError(t, (&auth_models.DeleteSessionInput{SessionID: 1}).Validate())
	assertBadRequest(t, (&auth_models.DeleteSessionInput{SessionID: 0}).Validate())
	assertBadRequest(t, (&auth_models.DeleteSessionInput{SessionID: -5}).Validate())
}
