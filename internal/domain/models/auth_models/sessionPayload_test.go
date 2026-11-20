package auth_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionsPayload_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilS *auth_models.SessionsPayload
	assertBadRequest(t, nilS.Validate())

	valid := func() auth_models.SessionsPayload {
		return auth_models.SessionsPayload{
			ID:                     1,
			UserID:                 2,
			MasterToken:            "master",
			FirebaseInstallationID: "fb-1",
			ExpiresAt:              1000,
		}
	}

	s := valid()
	require.NoError(t, s.Validate())

	cases := []struct {
		name   string
		mutate func(p *auth_models.SessionsPayload)
	}{
		{"non-positive id", func(p *auth_models.SessionsPayload) { p.ID = 0 }},
		{"non-positive user_id", func(p *auth_models.SessionsPayload) { p.UserID = 0 }},
		{"blank master_token", func(p *auth_models.SessionsPayload) { p.MasterToken = "  " }},
		{"blank firebase_installation_id", func(p *auth_models.SessionsPayload) { p.FirebaseInstallationID = "  " }},
		{"non-positive expires_at", func(p *auth_models.SessionsPayload) { p.ExpiresAt = 0 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := valid()
			tc.mutate(&p)
			assertBadRequest(t, p.Validate())
		})
	}
}
