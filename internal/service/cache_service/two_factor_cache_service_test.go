package cache_service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

func TestTwoFactorCacheService(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("Save and Get PreAuthSession success", func(t *testing.T) {
		session := auth_models.PreAuthSession{
			Token:  "token-123",
			UserID: 42,
		}

		if err := SavePreAuthSession(ctx, session); err != nil {
			t.Fatalf("SavePreAuthSession() unexpected error: %v", err)
		}

		res, err := GetPreAuthSession(ctx, "token-123")
		if err != nil {
			t.Fatalf("GetPreAuthSession() unexpected error: %v", err)
		}
		if res.UserID != 42 {
			t.Errorf("GetPreAuthSession() UserID = %d, want 42", res.UserID)
		}
	})

	t.Run("GetPreAuthSession MaxAttemptsReached triggers deletion and 403 Forbidden", func(t *testing.T) {
		session := auth_models.PreAuthSession{
			Token:    "token-max-attempts",
			UserID:   42,
			Attempts: variables.TwoFactorMaxAttempts,
		}
		_ = SavePreAuthSession(ctx, session)

		_, err := GetPreAuthSession(ctx, "token-max-attempts")
		if err == nil {
			t.Fatal("GetPreAuthSession() expected error on max attempts, got nil")
		}

		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusForbidden {
			t.Errorf("GetPreAuthSession() error = %v, want 403 Forbidden AppError", err)
		}

		// Vérifie la suppression automatique
		if _, getErr := GetPreAuthSession(ctx, "token-max-attempts"); getErr == nil {
			t.Error("expected session to be deleted after max attempts reached, but it was found")
		}
	})

	t.Run("IncrementPreAuthAttempts increments and enforces limit", func(t *testing.T) {
		session := auth_models.PreAuthSession{
			Token:    "token-incr",
			UserID:   42,
			Attempts: 1,
		}
		_ = SavePreAuthSession(ctx, session)

		if err := IncrementPreAuthAttempts(ctx, session); err != nil {
			t.Fatalf("IncrementPreAuthAttempts() unexpected error: %v", err)
		}

		res, _ := GetPreAuthSession(ctx, "token-incr")
		if res.Attempts != 2 {
			t.Errorf("expected 2 attempts, got %d", res.Attempts)
		}

		// Atteindre la limite
		session.Attempts = variables.TwoFactorMaxAttempts - 1
		errLimit := IncrementPreAuthAttempts(ctx, session)
		if errLimit == nil {
			t.Fatal("IncrementPreAuthAttempts() expected error when hitting limit, got nil")
		}

		var appErr *numan_error.AppError
		if !errors.As(errLimit, &appErr) || appErr.HTTPStatus != http.StatusForbidden {
			t.Errorf("IncrementPreAuthAttempts() error = %v, want 403 Forbidden AppError", errLimit)
		}
	})

	t.Run("DeletePreAuthSession completely removes session", func(t *testing.T) {
		session := auth_models.PreAuthSession{
			Token:  "token-delete",
			UserID: 42,
		}
		_ = SavePreAuthSession(ctx, session)

		if err := DeletePreAuthSession(ctx, "token-delete"); err != nil {
			t.Fatalf("DeletePreAuthSession() unexpected error: %v", err)
		}

		if _, err := GetPreAuthSession(ctx, "token-delete"); err == nil {
			t.Error("GetPreAuthSession() expected error after deletion, got nil")
		}
	})

	t.Run("ConsumeTOTPCode enforces idempotency within 90s window", func(t *testing.T) {
		userID := int64(42)
		code := "123456"

		ok, err := ConsumeTOTPCode(ctx, userID, code)
		if err != nil {
			t.Fatalf("ConsumeTOTPCode() first call unexpected error: %v", err)
		}
		if !ok {
			t.Error("ConsumeTOTPCode() first call = false, want true")
		}

		// Deuxième appel avec le même code doit être rejeté (idempotence)
		ok2, err2 := ConsumeTOTPCode(ctx, userID, code)
		if err2 != nil {
			t.Fatalf("ConsumeTOTPCode() second call unexpected error: %v", err2)
		}
		if ok2 {
			t.Error("ConsumeTOTPCode() second call = true, want false (replay attack protection)")
		}
	})

	t.Run("error propagation on Redis failure", func(t *testing.T) {
		srv.setFailCmd("SET", errors.New("redis set error"))
		defer srv.setFailCmd("SET", nil)

		err := SavePreAuthSession(ctx, auth_models.PreAuthSession{Token: "fail", UserID: 1})
		if err == nil {
			t.Fatal("SavePreAuthSession() error = nil, want non-nil on Redis failure")
		}
	})
}
