package two_factor_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertBadRequestPasskeyAssertion(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestPasskeyAssertionResponse_Validate(t *testing.T) {
	validAssert := func() two_factor_models.PasskeyAssertionResponse {
		return two_factor_models.PasskeyAssertionResponse{
			CredentialID:      "cred-1",
			AuthenticatorData: "auth-1",
			ClientDataJSON:    "client-1",
			Signature:         "sig-1",
		}
	}

	var nilA *two_factor_models.PasskeyAssertionResponse
	assertBadRequestPasskeyAssertion(t, nilA.Validate())

	a := validAssert()
	require.NoError(t, a.Validate())

	aMissingCred := validAssert()
	aMissingCred.CredentialID = "  "
	assertBadRequestPasskeyAssertion(t, aMissingCred.Validate())

	aMissingAuth := validAssert()
	aMissingAuth.AuthenticatorData = " "
	assertBadRequestPasskeyAssertion(t, aMissingAuth.Validate())

	aMissingClient := validAssert()
	aMissingClient.ClientDataJSON = ""
	assertBadRequestPasskeyAssertion(t, aMissingClient.Validate())

	aMissingSig := validAssert()
	aMissingSig.Signature = ""
	assertBadRequestPasskeyAssertion(t, aMissingSig.Validate())
}
