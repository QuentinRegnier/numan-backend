package auth_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoginInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilL *auth_models.LoginInput
	assertBadRequest(t, nilL.Validate())

	require.NoError(t, (&auth_models.LoginInput{
		Email:                  "alice@numan.app",
		PasswordHash:           "hash",
		FirebaseInstallationID: "fb-1",
	}).Validate())

	assertBadRequest(t, (&auth_models.LoginInput{Email: "bad", PasswordHash: "hash", FirebaseInstallationID: "fb-1"}).Validate())
	assertBadRequest(t, (&auth_models.LoginInput{Email: "alice@numan.app", PasswordHash: " ", FirebaseInstallationID: "fb-1"}).Validate())
	assertBadRequest(t, (&auth_models.LoginInput{Email: "alice@numan.app", PasswordHash: "hash", FirebaseInstallationID: ""}).Validate())
	assertBadRequest(t, (&auth_models.LoginInput{Email: "alice@numan.app", PasswordHash: "hash", FirebaseInstallationID: "   "}).Validate())
}
