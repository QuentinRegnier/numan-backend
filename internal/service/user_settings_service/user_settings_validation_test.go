package user_settings_service_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/user_settings_service"
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

func TestUserSettingsServices_InvalidPayloads_NoRedisSideEffects(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	initialKeys, err := repoRedis.ScanKeys(ctx, "*", 200)
	require.NoError(t, err)

	// 1. CheckUsernameAvailability rejects malformed usernames immediately
	assert.False(t, user_settings_service.CheckUsernameAvailability(ctx, ""))
	assert.False(t, user_settings_service.CheckUsernameAvailability(ctx, "ab"))
	assert.False(t, user_settings_service.CheckUsernameAvailability(ctx, "alice_99"))

	// 2. UpdateProfile validation
	_, err = user_settings_service.UpdateProfile(ctx, 0, user_settings_models.UpdateProfileInput{
		Username:  "Alice123",
		Email:     "alice@numan.app",
		FirstName: "Alice",
		LastName:  "Dupont",
	})
	assertBadRequest(t, err)

	_, err = user_settings_service.UpdateProfile(ctx, 10, user_settings_models.UpdateProfileInput{
		Username:  "bad_user",
		Email:     "alice@numan.app",
		FirstName: "Alice",
		LastName:  "Dupont",
	})
	assertBadRequest(t, err)

	// 3. UpdatePrivacy validation
	_, err = user_settings_service.UpdatePrivacy(ctx, 0, user_settings_models.UpdatePrivacyInput{})
	assertBadRequest(t, err)

	_, err = user_settings_service.UpdatePrivacy(ctx, 10, user_settings_models.UpdatePrivacyInput{
		PostVisibilityDefault: 3,
	})
	assertBadRequest(t, err)

	_, err = user_settings_service.UpdatePrivacy(ctx, 10, user_settings_models.UpdatePrivacyInput{
		ProfileVisibility: 9,
	})
	assertBadRequest(t, err)

	// 4. UpdateNotifications validation
	_, err = user_settings_service.UpdateNotifications(ctx, 0, user_settings_models.UpdateNotificationsInput{})
	assertBadRequest(t, err)

	// 5. UpdateDisplay validation
	_, err = user_settings_service.UpdateDisplay(ctx, 0, user_settings_models.UpdateDisplayInput{})
	assertBadRequest(t, err)

	_, err = user_settings_service.UpdateDisplay(ctx, 10, user_settings_models.UpdateDisplayInput{Theme: 3})
	assertBadRequest(t, err)

	_, err = user_settings_service.UpdateDisplay(ctx, 10, user_settings_models.UpdateDisplayInput{Language: -1})
	assertBadRequest(t, err)

	// Assert zero Redis or queue mutations occurred
	finalKeys, err := repoRedis.ScanKeys(ctx, "*", 200)
	require.NoError(t, err)
	assert.ElementsMatch(t, initialKeys, finalKeys)

	queueKeys, err := repoRedis.ScanKeys(ctx, "request_cache:q:*", 200)
	require.NoError(t, err)
	assert.Empty(t, queueKeys)
}
