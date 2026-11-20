package auth_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreAuthSession_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilP *auth_models.PreAuthSession
	assertBadRequest(t, nilP.Validate())

	require.NoError(t, (&auth_models.PreAuthSession{
		Token:            "tok",
		Flow:             "signup",
		UserID:           0,
		Method:           variables.TwoFactorMethodEmail,
		AvailableMethods: []int{variables.TwoFactorMethodEmail},
	}).Validate())

	require.NoError(t, (&auth_models.PreAuthSession{
		Token:            "tok",
		Flow:             "login",
		UserID:           10,
		Method:           variables.TwoFactorBitEmail | variables.TwoFactorBitTOTP,
		AvailableMethods: []int{variables.TwoFactorMethodEmail, variables.TwoFactorMethodTOTP},
	}).Validate())

	require.NoError(t, (&auth_models.PreAuthSession{
		Token:            "tok",
		Flow:             "setup",
		UserID:           10,
		Method:           variables.TwoFactorBitPasskey,
		AvailableMethods: []int{variables.TwoFactorMethodPasskey},
	}).Validate())

	assertBadRequest(t, (&auth_models.PreAuthSession{Token: " ", Flow: "login", UserID: 1}).Validate())
	assertBadRequest(t, (&auth_models.PreAuthSession{Token: "tok", Flow: "login", UserID: 0}).Validate())
	assertBadRequest(t, (&auth_models.PreAuthSession{Token: "tok", Flow: "setup", UserID: 0}).Validate())
	assertBadRequest(t, (&auth_models.PreAuthSession{Token: "tok", Flow: "signup", UserID: -1}).Validate())
	assertBadRequest(t, (&auth_models.PreAuthSession{Token: "tok", Flow: "unknown", UserID: 1}).Validate())
	assertBadRequest(t, (&auth_models.PreAuthSession{Token: "tok", Flow: "setup", UserID: 1, Method: -1}).Validate())
	assertBadRequest(t, (&auth_models.PreAuthSession{Token: "tok", Flow: "setup", UserID: 1, Method: 99}).Validate())
	assertBadRequest(t, (&auth_models.PreAuthSession{Token: "tok", Flow: "setup", UserID: 1, AvailableMethods: []int{99}}).Validate())
	assertBadRequest(t, (&auth_models.PreAuthSession{Token: "tok", Flow: "setup", UserID: 1, Attempts: -1}).Validate())
}
