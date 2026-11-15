package two_factor_service_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/two_factor_service"
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

func TestTwoFactorServices_InvalidPayloads_NoRedisSideEffects(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	initialKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)

	// SetupTwoFactor
	_, err = two_factor_service.SetupTwoFactor(ctx, 0, two_factor_models.SetupTwoFactorInput{Method: 1})
	assertBadRequest(t, err)
	_, err = two_factor_service.SetupTwoFactor(ctx, 1, two_factor_models.SetupTwoFactorInput{Method: 9})
	assertBadRequest(t, err)

	// ConfirmTwoFactorSetup
	assertBadRequest(t, two_factor_service.ConfirmTwoFactorSetup(ctx, 0, two_factor_models.ConfirmTwoFactorSetupInput{SetupToken: "t", Code: "123456"}))
	assertBadRequest(t, two_factor_service.ConfirmTwoFactorSetup(ctx, 1, two_factor_models.ConfirmTwoFactorSetupInput{SetupToken: " ", Code: "123456"}))
	assertBadRequest(t, two_factor_service.ConfirmTwoFactorSetup(ctx, 1, two_factor_models.ConfirmTwoFactorSetupInput{SetupToken: "t", Code: "123"}))

	// DisableTwoFactor
	assertBadRequest(t, two_factor_service.DisableTwoFactor(ctx, 0, two_factor_models.DisableTwoFactorInput{PasswordHash: "h", Method: 0}))
	assertBadRequest(t, two_factor_service.DisableTwoFactor(ctx, 1, two_factor_models.DisableTwoFactorInput{PasswordHash: "", Method: 0}))
	assertBadRequest(t, two_factor_service.DisableTwoFactor(ctx, 1, two_factor_models.DisableTwoFactorInput{PasswordHash: "h", Method: 5}))

	// DeletePasskey
	assertBadRequest(t, two_factor_service.DeletePasskey(ctx, 0, two_factor_models.DeletePasskeyInput{PasskeyID: 1}))
	assertBadRequest(t, two_factor_service.DeletePasskey(ctx, 1, two_factor_models.DeletePasskeyInput{PasskeyID: 0}))

	// GetUserPasskeys
	_, err = two_factor_service.GetUserPasskeys(ctx, 0)
	assertBadRequest(t, err)

	// SendTwoFactorEmailCode
	assertBadRequest(t, two_factor_service.SendTwoFactorEmailCode(ctx, two_factor_models.SendTwoFactorEmailInput{PreAuthToken: "  "}))

	// VerifyTwoFactorTOTP
	_, err = two_factor_service.VerifyTwoFactorTOTP(ctx, two_factor_models.VerifyTwoFactorTOTPInput{PreAuthToken: "t", Code: "12"})
	assertBadRequest(t, err)

	// VerifyTwoFactorEmail
	_, err = two_factor_service.VerifyTwoFactorEmail(ctx, two_factor_models.VerifyTwoFactorEmailInput{PreAuthToken: "", Code: "123456"})
	assertBadRequest(t, err)

	// VerifyTwoFactorPasskey
	_, err = two_factor_service.VerifyTwoFactorPasskey(ctx, two_factor_models.VerifyTwoFactorPasskeyInput{PreAuthToken: "t"})
	assertBadRequest(t, err)

	finalKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)
	assert.ElementsMatch(t, initialKeys, finalKeys)

	queueKeys, err := infraRedis.Rdb.Keys(ctx, "request_cache:q:*").Result()
	require.NoError(t, err)
	assert.Empty(t, queueKeys)
}
