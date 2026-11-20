package search_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/search_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutocompleteTextInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	require.NoError(t, (&search_models.AutocompleteTextInput{Query: "numan", Offset: 0, Limit: 10}).Validate())

	var nilPtr *search_models.AutocompleteTextInput
	assertBadRequest(t, nilPtr.Validate())
	assertBadRequest(t, (&search_models.AutocompleteTextInput{Query: "\t\n", Offset: 0, Limit: 10}).Validate())
	assertBadRequest(t, (&search_models.AutocompleteTextInput{Query: strings.Repeat("é", 101), Offset: 0, Limit: 10}).Validate())
	assertBadRequest(t, (&search_models.AutocompleteTextInput{Query: "ok", Offset: -1, Limit: 10}).Validate())
	assertBadRequest(t, (&search_models.AutocompleteTextInput{Query: "ok", Offset: 0, Limit: 51}).Validate())
}
