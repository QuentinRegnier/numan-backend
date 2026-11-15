package auth_service_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/auth_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertAuthBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestAuthServices_InvalidPayloads_NoRedisSideEffects(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	initialKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)

	// 1. CreateUser rejects malformed inputs before uniqueness or cache/queue calls
	_, err = auth_service.CreateUser(ctx, auth_models.SignUpInput{
		Username:               "ab",
		Email:                  "alice@numan.app",
		Phone:                  "+33612345678",
		PasswordHash:           "secret1234",
		Birthdate:              "15051995",
		FirebaseInstallationID: "fb-1",
	}, "127.0.0.1")
	assertAuthBadRequest(t, err)

	_, err = auth_service.CreateUser(ctx, auth_models.SignUpInput{
		Username:               "Alice123",
		Email:                  "alice@numan.app",
		Phone:                  "+33612345678",
		PasswordHash:           "secret1234",
		Birthdate:              "01012022", // < 13 years
		FirebaseInstallationID: "fb-1",
	}, "127.0.0.1")
	assertAuthBadRequest(t, err)

	// 2. Login rejects invalid LoginInput before DB queries
	_, err = auth_service.Login(ctx, auth_models.LoginInput{
		Email:                  "bad-email",
		PasswordHash:           "hash",
		FirebaseInstallationID: "fb-1",
	}, []string{"127.0.0.1"})
	assertAuthBadRequest(t, err)

	_, err = auth_service.Login(ctx, auth_models.LoginInput{
		Email:                  "alice@numan.app",
		PasswordHash:           "  ",
		FirebaseInstallationID: "fb-1",
	}, []string{"127.0.0.1"})
	assertAuthBadRequest(t, err)

	// 3. Logout rejects invalid callerID, preserves idempotent empty firebaseInstallationID
	assertAuthBadRequest(t, auth_service.Logout(ctx, 0, "fb-1"))
	require.NoError(t, auth_service.Logout(ctx, 1, ""))

	// 4. RevokeSession rejects invalid callerID or targetSessionID
	assertAuthBadRequest(t, auth_service.RevokeSession(ctx, 0, 10))
	assertAuthBadRequest(t, auth_service.RevokeSession(ctx, 1, 0))

	// 5. GetUserSessions rejects invalid userID
	_, err = auth_service.GetUserSessions(ctx, 0)
	assertAuthBadRequest(t, err)

	// 6. AddCertification rejects invalid userID or input before federation call
	_, err = auth_service.AddCertification(ctx, 0, auth_models.AddCertificationInput{
		Prestataire:     "federation_francaise",
		CertificationID: "LIC-12345",
	})
	assertAuthBadRequest(t, err)

	_, err = auth_service.AddCertification(ctx, 1, auth_models.AddCertificationInput{
		Prestataire:     "federation_francaise",
		CertificationID: "LIC INVALID!",
	})
	assertAuthBadRequest(t, err)

	finalKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)
	assert.ElementsMatch(t, initialKeys, finalKeys)

	queueKeys, err := infraRedis.Rdb.Keys(ctx, "request_cache:q:*").Result()
	require.NoError(t, err)
	assert.Empty(t, queueKeys)
}
