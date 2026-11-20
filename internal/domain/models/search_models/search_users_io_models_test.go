package search_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/search_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserSearchInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	require.NoError(t, (&search_models.UserSearchInput{Prefix: "alice", Offset: 0, Limit: 20}).Validate())

	var nilPtr *search_models.UserSearchInput
	assertBadRequest(t, nilPtr.Validate())
	assertBadRequest(t, (&search_models.UserSearchInput{Prefix: "", Offset: 0, Limit: 10}).Validate())
	assertBadRequest(t, (&search_models.UserSearchInput{Prefix: "alice", Offset: -5, Limit: 10}).Validate())
	assertBadRequest(t, (&search_models.UserSearchInput{Prefix: "alice", Offset: 0, Limit: 51}).Validate())
}
