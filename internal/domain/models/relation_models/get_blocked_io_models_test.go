package relation_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/relation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBlockedInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *relation_models.GetBlockedInput
	assertBadRequest(t, nilIn.Validate())
	require.NoError(t, (&relation_models.GetBlockedInput{Limit: 0, Offset: 0}).Validate())
	require.NoError(t, (&relation_models.GetBlockedInput{Limit: 100, Offset: 5}).Validate())
	assertBadRequest(t, (&relation_models.GetBlockedInput{Limit: -1}).Validate())
	assertBadRequest(t, (&relation_models.GetBlockedInput{Limit: 101}).Validate())
	assertBadRequest(t, (&relation_models.GetBlockedInput{Offset: -1}).Validate())
}
