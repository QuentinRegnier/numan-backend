package two_factor_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertBadRequestConfirmTwoFactorSetup(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestConfirmTwoFactorSetupInput_Validate(t *testing.T) {
	validReg := func() two_factor_models.PasskeyRegistrationResponse {
		return two_factor_models.PasskeyRegistrationResponse{
			CredentialID:      "cred-1",
			PublicKeyCOSE:     "cose-1",
			AuthenticatorData: "auth-1",
			ClientDataJSON:    "client-1",
			DeviceName:        "MacBook Pro",
		}
	}

	var nilC *two_factor_models.ConfirmTwoFactorSetupInput
	assertBadRequestConfirmTwoFactorSetup(t, nilC.Validate())

	require.NoError(t, (&two_factor_models.ConfirmTwoFactorSetupInput{SetupToken: "tok", Code: "123456"}).Validate())
	reg := validReg()
	require.NoError(t, (&two_factor_models.ConfirmTwoFactorSetupInput{SetupToken: "tok", PasskeyRegistration: &reg}).Validate())

	assertBadRequestConfirmTwoFactorSetup(t, (&two_factor_models.ConfirmTwoFactorSetupInput{SetupToken: " ", Code: "123456"}).Validate())
	assertBadRequestConfirmTwoFactorSetup(t, (&two_factor_models.ConfirmTwoFactorSetupInput{SetupToken: "tok"}).Validate())
	assertBadRequestConfirmTwoFactorSetup(t, (&two_factor_models.ConfirmTwoFactorSetupInput{SetupToken: "tok", Code: "12345"}).Validate())
	assertBadRequestConfirmTwoFactorSetup(t, (&two_factor_models.ConfirmTwoFactorSetupInput{SetupToken: "tok", Code: "12a456"}).Validate())

	badReg := validReg()
	badReg.PublicKeyCOSE = "   "
	assertBadRequestConfirmTwoFactorSetup(t, (&two_factor_models.ConfirmTwoFactorSetupInput{SetupToken: "tok", PasskeyRegistration: &badReg}).Validate())
}
