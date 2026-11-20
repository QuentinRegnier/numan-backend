package two_factor_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertBadRequestDisableTwoFactor(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestDisableTwoFactorInput_Validate(t *testing.T) {
	var nilDis *two_factor_models.DisableTwoFactorInput
	assertBadRequestDisableTwoFactor(t, nilDis.Validate())

	require.NoError(t, (&two_factor_models.DisableTwoFactorInput{PasswordHash: "hash", Method: variables.TwoFactorMethodNone}).Validate())
	require.NoError(t, (&two_factor_models.DisableTwoFactorInput{PasswordHash: "hash", Method: variables.TwoFactorMethodEmail}).Validate())
	require.NoError(t, (&two_factor_models.DisableTwoFactorInput{PasswordHash: "hash", Method: variables.TwoFactorMethodTOTP}).Validate())
	require.NoError(t, (&two_factor_models.DisableTwoFactorInput{PasswordHash: "hash", Method: variables.TwoFactorMethodPasskey}).Validate())

	assertBadRequestDisableTwoFactor(t, (&two_factor_models.DisableTwoFactorInput{PasswordHash: "  ", Method: 1}).Validate())
	assertBadRequestDisableTwoFactor(t, (&two_factor_models.DisableTwoFactorInput{PasswordHash: "hash", Method: 4}).Validate())
	assertBadRequestDisableTwoFactor(t, (&two_factor_models.DisableTwoFactorInput{PasswordHash: "hash", Method: -1}).Validate())
}
