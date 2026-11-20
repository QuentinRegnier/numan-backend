package variables_models_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/variables_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestUpdateVariablesInput_Validate(t *testing.T) {
	valid := variables_models.UpdateVariablesInput{
		"DefaultGlobalSearchLimit": json.RawMessage(`20`),
	}
	require.NoError(t, valid.Validate())

	var nilPtr *variables_models.UpdateVariablesInput
	assertBadRequest(t, nilPtr.Validate())

	empty := variables_models.UpdateVariablesInput{}
	assertBadRequest(t, empty.Validate())

	unknown := variables_models.UpdateVariablesInput{
		"NonExistentVariable": json.RawMessage(`1`),
	}
	assertBadRequest(t, unknown.Validate())

	outOfRange := variables_models.UpdateVariablesInput{
		"DefaultGlobalSearchLimit": json.RawMessage(`-5`),
	}
	assertBadRequest(t, outOfRange.Validate())
}
