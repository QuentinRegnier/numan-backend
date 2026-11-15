package two_factor_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
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

func TestTwoFactorModels_Validate(t *testing.T) {
	validReg := func() two_factor_models.PasskeyRegistrationResponse {
		return two_factor_models.PasskeyRegistrationResponse{
			CredentialID:      "cred-1",
			PublicKeyCOSE:     "cose-1",
			AuthenticatorData: "auth-1",
			ClientDataJSON:    "client-1",
			DeviceName:        "MacBook Pro",
		}
	}
	validAssert := func() two_factor_models.PasskeyAssertionResponse {
		return two_factor_models.PasskeyAssertionResponse{
			CredentialID:      "cred-1",
			AuthenticatorData: "auth-1",
			ClientDataJSON:    "client-1",
			Signature:         "sig-1",
		}
	}

	t.Run("PasskeyRegistrationResponse", func(t *testing.T) {
		var nilR *two_factor_models.PasskeyRegistrationResponse
		assertBadRequest(t, nilR.Validate())
		r := validReg()
		require.NoError(t, r.Validate())
		rBad := validReg()
		rBad.DeviceName = strings.Repeat("é", 65)
		assertBadRequest(t, rBad.Validate())
		rMissing := validReg()
		rMissing.PublicKeyCOSE = "   "
		assertBadRequest(t, rMissing.Validate())
	})

	t.Run("PasskeyAssertionResponse", func(t *testing.T) {
		var nilA *two_factor_models.PasskeyAssertionResponse
		assertBadRequest(t, nilA.Validate())
		a := validAssert()
		require.NoError(t, a.Validate())
		aMissing := validAssert()
		aMissing.Signature = ""
		assertBadRequest(t, aMissing.Validate())
	})

	t.Run("SetupTwoFactorInput", func(t *testing.T) {
		var nilS *two_factor_models.SetupTwoFactorInput
		assertBadRequest(t, nilS.Validate())
		require.NoError(t, (&two_factor_models.SetupTwoFactorInput{Method: variables.TwoFactorMethodTOTP, DeviceName: "Phone"}).Validate())
		assertBadRequest(t, (&two_factor_models.SetupTwoFactorInput{Method: 0}).Validate())
		assertBadRequest(t, (&two_factor_models.SetupTwoFactorInput{Method: 4}).Validate())
		assertBadRequest(t, (&two_factor_models.SetupTwoFactorInput{Method: 1, DeviceName: strings.Repeat("a", 65)}).Validate())
	})

	t.Run("ConfirmTwoFactorSetupInput", func(t *testing.T) {
		var nilC *two_factor_models.ConfirmTwoFactorSetupInput
		assertBadRequest(t, nilC.Validate())
		require.NoError(t, (&two_factor_models.ConfirmTwoFactorSetupInput{SetupToken: "tok", Code: "123456"}).Validate())
		reg := validReg()
		require.NoError(t, (&two_factor_models.ConfirmTwoFactorSetupInput{SetupToken: "tok", PasskeyRegistration: &reg}).Validate())
		assertBadRequest(t, (&two_factor_models.ConfirmTwoFactorSetupInput{SetupToken: " ", Code: "123456"}).Validate())
		assertBadRequest(t, (&two_factor_models.ConfirmTwoFactorSetupInput{SetupToken: "tok"}).Validate())
		assertBadRequest(t, (&two_factor_models.ConfirmTwoFactorSetupInput{SetupToken: "tok", Code: "12345"}).Validate())
		assertBadRequest(t, (&two_factor_models.ConfirmTwoFactorSetupInput{SetupToken: "tok", Code: "12a456"}).Validate())
	})

	t.Run("VerifyAndEmailInputs", func(t *testing.T) {
		var nilE *two_factor_models.VerifyTwoFactorEmailInput
		assertBadRequest(t, nilE.Validate())
		require.NoError(t, (&two_factor_models.VerifyTwoFactorEmailInput{PreAuthToken: "t", Code: "000123"}).Validate())
		assertBadRequest(t, (&two_factor_models.VerifyTwoFactorEmailInput{PreAuthToken: "", Code: "123456"}).Validate())
		assertBadRequest(t, (&two_factor_models.VerifyTwoFactorEmailInput{PreAuthToken: "t", Code: "abc"}).Validate())

		var nilT *two_factor_models.VerifyTwoFactorTOTPInput
		assertBadRequest(t, nilT.Validate())
		require.NoError(t, (&two_factor_models.VerifyTwoFactorTOTPInput{PreAuthToken: "t", Code: "654321"}).Validate())
		assertBadRequest(t, (&two_factor_models.VerifyTwoFactorTOTPInput{PreAuthToken: "t", Code: "65432"}).Validate())

		var nilSend *two_factor_models.SendTwoFactorEmailInput
		assertBadRequest(t, nilSend.Validate())
		require.NoError(t, (&two_factor_models.SendTwoFactorEmailInput{PreAuthToken: "t"}).Validate())
		assertBadRequest(t, (&two_factor_models.SendTwoFactorEmailInput{PreAuthToken: "  "}).Validate())

		var nilP *two_factor_models.VerifyTwoFactorPasskeyInput
		assertBadRequest(t, nilP.Validate())
		require.NoError(t, (&two_factor_models.VerifyTwoFactorPasskeyInput{PreAuthToken: "t", PasskeyAssertion: validAssert()}).Validate())
		require.NoError(t, (&two_factor_models.VerifyTwoFactorPasskeyInput{PreAuthToken: "t", PasskeyRegistration: validReg()}).Validate())
		assertBadRequest(t, (&two_factor_models.VerifyTwoFactorPasskeyInput{PreAuthToken: "t"}).Validate())
	})

	t.Run("DeleteAndDisableAndPasskeyPayload", func(t *testing.T) {
		var nilDel *two_factor_models.DeletePasskeyInput
		assertBadRequest(t, nilDel.Validate())
		require.NoError(t, (&two_factor_models.DeletePasskeyInput{PasskeyID: 1}).Validate())
		assertBadRequest(t, (&two_factor_models.DeletePasskeyInput{PasskeyID: 0}).Validate())

		var nilDis *two_factor_models.DisableTwoFactorInput
		assertBadRequest(t, nilDis.Validate())
		require.NoError(t, (&two_factor_models.DisableTwoFactorInput{PasswordHash: "hash", Method: 0}).Validate())
		require.NoError(t, (&two_factor_models.DisableTwoFactorInput{PasswordHash: "hash", Method: 3}).Validate())
		assertBadRequest(t, (&two_factor_models.DisableTwoFactorInput{PasswordHash: "  ", Method: 1}).Validate())
		assertBadRequest(t, (&two_factor_models.DisableTwoFactorInput{PasswordHash: "hash", Method: 4}).Validate())

		var nilPk *two_factor_models.PasskeyPayload
		assertBadRequest(t, nilPk.Validate())
		pk := two_factor_models.PasskeyPayload{
			ID:           1,
			UserID:       2,
			CredentialID: "cred",
			PublicKey:    []byte{1, 2, 3},
			SignCount:    0,
			DeviceName:   "iPhone",
		}
		require.NoError(t, pk.Validate())
		pkBad := pk
		pkBad.SignCount = -1
		assertBadRequest(t, pkBad.Validate())
		pkNoKey := pk
		pkNoKey.PublicKey = nil
		assertBadRequest(t, pkNoKey.Validate())
	})
}
