package user_settings_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckUsernameInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *user_settings_models.CheckUsernameInput
	assertBadRequest(t, nilIn.Validate())
	require.NoError(t, (&user_settings_models.CheckUsernameInput{Username: "Alice99"}).Validate())
	assertBadRequest(t, (&user_settings_models.CheckUsernameInput{Username: "ab"}).Validate())
	assertBadRequest(t, (&user_settings_models.CheckUsernameInput{Username: strings.Repeat("a", 31)}).Validate())
	assertBadRequest(t, (&user_settings_models.CheckUsernameInput{Username: "alice_99"}).Validate())
	assertBadRequest(t, (&user_settings_models.CheckUsernameInput{Username: "ééé"}).Validate())
}
