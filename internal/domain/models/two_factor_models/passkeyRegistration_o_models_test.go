package two_factor_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertBadRequestPasskeyRegistration(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestPasskeyRegistrationResponse_Validate(t *testing.T) {
	validReg := func() two_factor_models.PasskeyRegistrationResponse {
		return two_factor_models.PasskeyRegistrationResponse{
			CredentialID:      "cred-1",
			PublicKeyCOSE:     "cose-1",
			AuthenticatorData: "auth-1",
			ClientDataJSON:    "client-1",
			DeviceName:        "MacBook Pro",
		}
	}

	var nilR *two_factor_models.PasskeyRegistrationResponse
	assertBadRequestPasskeyRegistration(t, nilR.Validate())

	r := validReg()
	require.NoError(t, r.Validate())

	rMaxDevice := validReg()
	rMaxDevice.DeviceName = strings.Repeat("é", 64)
	require.NoError(t, rMaxDevice.Validate())

	rBad := validReg()
	rBad.DeviceName = strings.Repeat("é", 65)
	assertBadRequestPasskeyRegistration(t, rBad.Validate())

	rNoCred := validReg()
	rNoCred.CredentialID = "  "
	assertBadRequestPasskeyRegistration(t, rNoCred.Validate())

	rMissing := validReg()
	rMissing.PublicKeyCOSE = "   "
	assertBadRequestPasskeyRegistration(t, rMissing.Validate())

	rNoAuth := validReg()
	rNoAuth.AuthenticatorData = ""
	assertBadRequestPasskeyRegistration(t, rNoAuth.Validate())

	rNoClient := validReg()
	rNoClient.ClientDataJSON = " "
	assertBadRequestPasskeyRegistration(t, rNoClient.Validate())
}
