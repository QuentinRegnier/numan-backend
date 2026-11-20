package profile_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/profile_models"
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

func TestGetProfileInput_Validate(t *testing.T) {
	var nilInput *profile_models.GetProfileInput
	assertBadRequest(t, nilInput.Validate())

	cases := []struct {
		name  string
		input profile_models.GetProfileInput
		valid bool
	}{
		{name: "zero values (self default)", input: profile_models.GetProfileInput{}, valid: true},
		{name: "max valid boundary", input: profile_models.GetProfileInput{TargetID: 123, Limit: 50, Offset: 10}, valid: true},
		{name: "negative target_id", input: profile_models.GetProfileInput{TargetID: -1}},
		{name: "negative limit", input: profile_models.GetProfileInput{Limit: -1}},
		{name: "limit > 50", input: profile_models.GetProfileInput{Limit: 51}},
		{name: "negative offset", input: profile_models.GetProfileInput{Offset: -1}},
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
