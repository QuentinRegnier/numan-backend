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

func assertBadRequestSetupTwoFactor(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestSetupTwoFactorInput_Validate(t *testing.T) {
	var nilS *two_factor_models.SetupTwoFactorInput
	assertBadRequestSetupTwoFactor(t, nilS.Validate())

	require.NoError(t, (&two_factor_models.SetupTwoFactorInput{Method: variables.TwoFactorMethodEmail}).Validate())
	require.NoError(t, (&two_factor_models.SetupTwoFactorInput{Method: variables.TwoFactorMethodTOTP, DeviceName: "Phone"}).Validate())
	require.NoError(t, (&two_factor_models.SetupTwoFactorInput{Method: variables.TwoFactorMethodPasskey, DeviceName: strings.Repeat("é", 64)}).Validate())

	assertBadRequestSetupTwoFactor(t, (&two_factor_models.SetupTwoFactorInput{Method: 0}).Validate())
	assertBadRequestSetupTwoFactor(t, (&two_factor_models.SetupTwoFactorInput{Method: 4}).Validate())
	assertBadRequestSetupTwoFactor(t, (&two_factor_models.SetupTwoFactorInput{Method: 1, DeviceName: strings.Repeat("a", 65)}).Validate())
}
