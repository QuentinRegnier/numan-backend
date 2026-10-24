package cache_service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTwoFactorCacheService(t *testing.T) {
	ctx := context.Background()

	t.Run("SavePreAuthSession, GetPreAuthSession, IncrementPreAuthAttempts, DeletePreAuthSession", func(t *testing.T) {
		srv := test.SetupTestRedis(t)

		sess := auth_models.PreAuthSession{
			Token:    "tok-123",
			UserID:   42,
			Attempts: 0,
		}
		require.NoError(t, SavePreAuthSession(ctx, sess))

		got, err := GetPreAuthSession(ctx, "tok-123")
		require.NoError(t, err)
		assert.Equal(t, int64(42), got.UserID)

		// Increment below max attempts
		require.NoError(t, IncrementPreAuthAttempts(ctx, got))
		got, err = GetPreAuthSession(ctx, "tok-123")
		require.NoError(t, err)
		assert.Equal(t, 1, got.Attempts)

		// Increment reaching max attempts revokes session
		got.Attempts = variables.TwoFactorMaxAttempts - 1
		err = IncrementPreAuthAttempts(ctx, got)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus)

		// GetPreAuthSession when attempts >= max attempts deletes and returns Forbidden
		maxedSess := auth_models.PreAuthSession{Token: "tok-max", UserID: 42, Attempts: variables.TwoFactorMaxAttempts}
		require.NoError(t, SavePreAuthSession(ctx, maxedSess))
		srv.SetFailCmd("DEL", errors.New("del warn"))
		_, err = GetPreAuthSession(ctx, "tok-max")
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus)
		_ = IncrementPreAuthAttempts(ctx, maxedSess)
		srv.SetFailCmd("DEL", nil)

		require.NoError(t, DeletePreAuthSession(ctx, "tok-max"))

		// Expired / non-existent token
		_, err = GetPreAuthSession(ctx, "missing")
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeTwoFactorExpired, appErr.Code)
	})

	t.Run("ConsumeTOTPCode", func(t *testing.T) {
		srv := test.SetupTestRedis(t)

		ok, err := ConsumeTOTPCode(ctx, 42, "123456")
		require.NoError(t, err)
		assert.True(t, ok)

		// Replay returns false
		ok, err = ConsumeTOTPCode(ctx, 42, "123456")
		require.NoError(t, err)
		assert.False(t, ok)

		// Error path
		var appErr *numan_error.AppError
		srv.SetFailCmd("SET", errors.New("set err"))
		_, err = ConsumeTOTPCode(ctx, 42, "654321")
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
	})

	t.Run("Error Paths", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		var appErr *numan_error.AppError

		sess := auth_models.PreAuthSession{Token: "tok-err", UserID: 1, Attempts: 0}
		srv.SetFailCmd("SET", errors.New("set err"))
		require.ErrorAs(t, SavePreAuthSession(ctx, sess), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)

		require.ErrorAs(t, IncrementPreAuthAttempts(ctx, sess), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("SET", nil)

		srv.SetFailCmd("DEL", errors.New("del err"))
		require.ErrorAs(t, DeletePreAuthSession(ctx, "tok-err"), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
	})
}
