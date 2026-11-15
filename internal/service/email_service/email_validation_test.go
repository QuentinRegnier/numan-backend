package email_service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertEmailBadRequest(t *testing.T, err error) {
	t.Helper()
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestEmailService_ValidationGuardsAndSuccessEnqueue(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	// Invalid inputs in DispatchEmail & DispatchSecurityEmail must reject with 400 and never enqueue
	invalidCases := []struct {
		name     string
		email    string
		template string
		subject  string
	}{
		{"invalid_email", "not-an-email", "welcome", "Bienvenue"},
		{"email_with_display_name", "Alice <alice@example.com>", "welcome", "Bienvenue"},
		{"email_crlf", "alice@example.com\r\nBcc:evil@example.com", "welcome", "Bienvenue"},
		{"template_empty", "alice@example.com", "   ", "Bienvenue"},
		{"template_traversal", "alice@example.com", "../secret", "Bienvenue"},
		{"template_slash", "alice@example.com", "auth/welcome", "Bienvenue"},
		{"subject_empty", "alice@example.com", "welcome", "   "},
		{"subject_crlf_injection", "alice@example.com", "welcome", "Code\r\nBcc: evil@example.com"},
		{"subject_too_long", "alice@example.com", "welcome", strings.Repeat("a", 256)},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			err := DispatchEmail(ctx, 10, tc.email, tc.template, tc.subject, map[string]any{"k": "v"})
			assertEmailBadRequest(t, err)

			errSec := DispatchSecurityEmail(ctx, tc.email, tc.template, tc.subject, map[string]any{"k": "v"})
			assertEmailBadRequest(t, errSec)
		})
	}

	keys, errKeys := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, errKeys)
	assert.Empty(t, keys, "Aucun payload email invalide ne doit atteindre Redis")

	// Valid email enqueues cleanly (with userID=0 bypassing privacy cache)
	require.NoError(t, DispatchEmail(ctx, 0, "alice@example.com", "welcome", "Bienvenue sur NUMAN", map[string]any{"name": "Alice"}))
	raw, errPop := redis.WorkerQueue.LPop(ctx, variables.EmailWorkerQueueName)
	require.NoError(t, errPop)
	assert.Contains(t, raw, `"recipient_email":"alice@example.com"`)
	assert.Contains(t, raw, `"template_name":"welcome"`)

	// Valid security email enqueues cleanly (via raw Client.LPush on variables.EmailWorkerQueueName)
	require.NoError(t, DispatchSecurityEmail(ctx, "bob@example.com", "otp_2fa", "Votre code 2FA", map[string]any{"code": "123456"}))
	rawSec, errPopSec := infraRedis.Rdb.LPop(ctx, variables.EmailWorkerQueueName).Result()
	require.NoError(t, errPopSec)
	assert.Contains(t, rawSec, `"recipient_email":"bob@example.com"`)
}
