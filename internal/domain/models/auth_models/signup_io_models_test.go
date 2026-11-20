package auth_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignUpInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *auth_models.SignUpInput
	assertBadRequest(t, nilIn.Validate())

	valid := func() auth_models.SignUpInput {
		return auth_models.SignUpInput{
			Username:               "Alice123",
			Email:                  "alice@numan.app",
			Phone:                  "+33612345678",
			PasswordHash:           "secretPass123",
			FirstName:              "Alice",
			LastName:               "Dupont",
			Birthdate:              "15051995",
			Gender:                 1,
			Bio:                    "Sport",
			FirebaseInstallationID: "fb-inst-123",
		}
	}

	v := valid()
	require.NoError(t, v.Validate())

	cases := []struct {
		name   string
		mutate func(p *auth_models.SignUpInput)
	}{
		{"invalid username short", func(p *auth_models.SignUpInput) { p.Username = "ab" }},
		{"invalid username non-alnum", func(p *auth_models.SignUpInput) { p.Username = "alice_1" }},
		{"invalid email", func(p *auth_models.SignUpInput) { p.Email = "not-an-email" }},
		{"invalid phone", func(p *auth_models.SignUpInput) { p.Phone = "0612345678" }},
		{"password too short", func(p *auth_models.SignUpInput) { p.PasswordHash = "1234567" }},
		{"password whitespace", func(p *auth_models.SignUpInput) { p.PasswordHash = "        " }},
		{"first name > 50 runes", func(p *auth_models.SignUpInput) { p.FirstName = strings.Repeat("é", 51) }},
		{"last name > 50 runes", func(p *auth_models.SignUpInput) { p.LastName = strings.Repeat("é", 51) }},
		{"birthdate invalid format", func(p *auth_models.SignUpInput) { p.Birthdate = "19950515" }},
		{"birthdate non-digits", func(p *auth_models.SignUpInput) { p.Birthdate = "1505abcd" }},
		{"birthdate under 13", func(p *auth_models.SignUpInput) { p.Birthdate = "01012022" }},
		{"birthdate over 120", func(p *auth_models.SignUpInput) { p.Birthdate = "01011890" }},
		{"invalid gender", func(p *auth_models.SignUpInput) { p.Gender = 3 }},
		{"bio > 500 runes", func(p *auth_models.SignUpInput) { p.Bio = strings.Repeat("é", 501) }},
		{"location > 100 runes", func(p *auth_models.SignUpInput) { p.Location = strings.Repeat("é", 101) }},
		{"school > 100 runes", func(p *auth_models.SignUpInput) { p.School = strings.Repeat("é", 101) }},
		{"work > 100 runes", func(p *auth_models.SignUpInput) { p.Work = strings.Repeat("é", 101) }},
		{"blank firebase_installation_id", func(p *auth_models.SignUpInput) { p.FirebaseInstallationID = "  " }},
		{"invalid nested display theme", func(p *auth_models.SignUpInput) { p.DisplayAndContent.Theme = 5 }},
		{"invalid nested privacy visibility", func(p *auth_models.SignUpInput) { p.Privacy.PostVisibilityDefault = 3 }},
		{"negative profile_picture_id", func(p *auth_models.SignUpInput) { p.ProfilePictureID = -1 }},
		{"invalid two_factor_method", func(p *auth_models.SignUpInput) { p.TwoFactorMethod = 9 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := valid()
			tc.mutate(&p)
			assertBadRequest(t, p.Validate())
		})
	}
}
