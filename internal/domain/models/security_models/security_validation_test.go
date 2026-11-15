package security_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/security_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
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

func TestRefreshMasterInput_Validate(t *testing.T) {
	var nilInput *security_models.RefreshMasterInput
	assertBadRequest(t, nilInput.Validate())

	cases := []struct {
		name  string
		input security_models.RefreshMasterInput
		valid bool
	}{
		{
			name:  "valid input",
			input: security_models.RefreshMasterInput{UserID: 42, MasterToken: "tok-123", Username: "alice"},
			valid: true,
		},
		{
			name:  "zero user_id",
			input: security_models.RefreshMasterInput{UserID: 0, MasterToken: "tok-123", Username: "alice"},
		},
		{
			name:  "negative user_id",
			input: security_models.RefreshMasterInput{UserID: -1, MasterToken: "tok-123", Username: "alice"},
		},
		{
			name:  "blank master_token",
			input: security_models.RefreshMasterInput{UserID: 42, MasterToken: "   ", Username: "alice"},
		},
		{
			name:  "blank username",
			input: security_models.RefreshMasterInput{UserID: 42, MasterToken: "tok-123", Username: "\t"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.input
			if tc.valid {
				require.NoError(t, in.Validate())
			} else {
				assertBadRequest(t, in.Validate())
			}
		})
	}
}
