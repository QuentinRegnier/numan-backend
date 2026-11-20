package two_factor_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertBadRequestVerifyTwoFactor(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestVerifyTwoFactorEmailInput_Validate(t *testing.T) {
	var nilE *two_factor_models.VerifyTwoFactorEmailInput
	assertBadRequestVerifyTwoFactor(t, nilE.Validate())

	require.NoError(t, (&two_factor_models.VerifyTwoFactorEmailInput{PreAuthToken: "t", Code: "000123"}).Validate())
	assertBadRequestVerifyTwoFactor(t, (&two_factor_models.VerifyTwoFactorEmailInput{PreAuthToken: "", Code: "123456"}).Validate())
	assertBadRequestVerifyTwoFactor(t, (&two_factor_models.VerifyTwoFactorEmailInput{PreAuthToken: "t", Code: "abc"}).Validate())
	assertBadRequestVerifyTwoFactor(t, (&two_factor_models.VerifyTwoFactorEmailInput{PreAuthToken: "t", Code: "12345"}).Validate())
}

func TestVerifyTwoFactorTOTPInput_Validate(t *testing.T) {
	var nilT *two_factor_models.VerifyTwoFactorTOTPInput
	assertBadRequestVerifyTwoFactor(t, nilT.Validate())

	require.NoError(t, (&two_factor_models.VerifyTwoFactorTOTPInput{PreAuthToken: "t", Code: "654321"}).Validate())
	assertBadRequestVerifyTwoFactor(t, (&two_factor_models.VerifyTwoFactorTOTPInput{PreAuthToken: "   ", Code: "654321"}).Validate())
	assertBadRequestVerifyTwoFactor(t, (&two_factor_models.VerifyTwoFactorTOTPInput{PreAuthToken: "t", Code: "65432"}).Validate())
	assertBadRequestVerifyTwoFactor(t, (&two_factor_models.VerifyTwoFactorTOTPInput{PreAuthToken: "t", Code: "65a321"}).Validate())
}

func TestSendTwoFactorEmailInput_Validate(t *testing.T) {
	var nilSend *two_factor_models.SendTwoFactorEmailInput
	assertBadRequestVerifyTwoFactor(t, nilSend.Validate())

	require.NoError(t, (&two_factor_models.SendTwoFactorEmailInput{PreAuthToken: "t"}).Validate())
	assertBadRequestVerifyTwoFactor(t, (&two_factor_models.SendTwoFactorEmailInput{PreAuthToken: "  "}).Validate())
}

func TestVerifyTwoFactorPasskeyInput_Validate(t *testing.T) {
	validReg := two_factor_models.PasskeyRegistrationResponse{
		CredentialID:      "cred-1",
		PublicKeyCOSE:     "cose-1",
		AuthenticatorData: "auth-1",
		ClientDataJSON:    "client-1",
		DeviceName:        "MacBook Pro",
	}
	validAssert := two_factor_models.PasskeyAssertionResponse{
		CredentialID:      "cred-1",
		AuthenticatorData: "auth-1",
		ClientDataJSON:    "client-1",
		Signature:         "sig-1",
	}

	var nilP *two_factor_models.VerifyTwoFactorPasskeyInput
	assertBadRequestVerifyTwoFactor(t, nilP.Validate())

	require.NoError(t, (&two_factor_models.VerifyTwoFactorPasskeyInput{PreAuthToken: "t", PasskeyAssertion: validAssert}).Validate())
	require.NoError(t, (&two_factor_models.VerifyTwoFactorPasskeyInput{PreAuthToken: "t", PasskeyRegistration: validReg}).Validate())
	assertBadRequestVerifyTwoFactor(t, (&two_factor_models.VerifyTwoFactorPasskeyInput{PreAuthToken: "  ", PasskeyAssertion: validAssert}).Validate())
	assertBadRequestVerifyTwoFactor(t, (&two_factor_models.VerifyTwoFactorPasskeyInput{PreAuthToken: "t"}).Validate())

	badAssert := validAssert
	badAssert.Signature = ""
	assertBadRequestVerifyTwoFactor(t, (&two_factor_models.VerifyTwoFactorPasskeyInput{PreAuthToken: "t", PasskeyAssertion: badAssert}).Validate())

	badReg := validReg
	badReg.PublicKeyCOSE = ""
	assertBadRequestVerifyTwoFactor(t, (&two_factor_models.VerifyTwoFactorPasskeyInput{PreAuthToken: "t", PasskeyRegistration: badReg}).Validate())
}
