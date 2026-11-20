package user_settings_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateProfileInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *user_settings_models.UpdateProfileInput
	assertBadRequest(t, nilIn.Validate())

	valid := func() user_settings_models.UpdateProfileInput {
		return user_settings_models.UpdateProfileInput{
			Username:  "Alice123",
			Email:     "alice@numan.app",
			Phone:     "+33612345678",
			FirstName: "Alice",
			LastName:  "Dupont",
			Bio:       "Passionnée de sport",
		}
	}
	v := valid()
	require.NoError(t, v.Validate())
	vNoPhone := valid()
	vNoPhone.Phone = ""
	require.NoError(t, vNoPhone.Validate())

	cases := []struct {
		name   string
		mutate func(p *user_settings_models.UpdateProfileInput)
	}{
		{"invalid username", func(p *user_settings_models.UpdateProfileInput) { p.Username = "a_b" }},
		{"invalid email", func(p *user_settings_models.UpdateProfileInput) { p.Email = "bad-email" }},
		{"invalid e164 phone", func(p *user_settings_models.UpdateProfileInput) { p.Phone = "0612345678" }},
		{"blank first name", func(p *user_settings_models.UpdateProfileInput) { p.FirstName = "   " }},
		{"first name > 50 runes", func(p *user_settings_models.UpdateProfileInput) { p.FirstName = strings.Repeat("é", 51) }},
		{"blank last name", func(p *user_settings_models.UpdateProfileInput) { p.LastName = "" }},
		{"last name > 50 runes", func(p *user_settings_models.UpdateProfileInput) { p.LastName = strings.Repeat("é", 51) }},
		{"bio > 500 runes", func(p *user_settings_models.UpdateProfileInput) { p.Bio = strings.Repeat("é", 501) }},
		{"location > 100 runes", func(p *user_settings_models.UpdateProfileInput) { p.Location = strings.Repeat("é", 101) }},
		{"school > 100 runes", func(p *user_settings_models.UpdateProfileInput) { p.School = strings.Repeat("é", 101) }},
		{"work > 100 runes", func(p *user_settings_models.UpdateProfileInput) { p.Work = strings.Repeat("é", 101) }},
		{"negative profile picture id", func(p *user_settings_models.UpdateProfileInput) { p.ProfilePictureID = -1 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := valid()
			tc.mutate(&p)
			assertBadRequest(t, p.Validate())
		})
	}
}
