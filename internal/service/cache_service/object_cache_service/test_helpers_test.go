package object_cache_service

import (
	"errors"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

func assertAppError(t testing.TB, err error, wantStatus int, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected AppError (status=%d, code=%s), got nil", wantStatus, wantCode)
	}
	var appErr *numan_error.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *numan_error.AppError, got %T: %v", err, err)
	}
	if appErr.HTTPStatus != wantStatus || appErr.Code != wantCode {
		t.Errorf("got AppError(status=%d, code=%s), want (status=%d, code=%s)", appErr.HTTPStatus, appErr.Code, wantStatus, wantCode)
	}
}

func waitForCondition(t testing.TB, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("condition not met within %v", timeout)
	}
}
