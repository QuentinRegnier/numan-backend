package auth_models_test

import (
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
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

func TestSignUpInput_Validate(t *testing.T) {
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
		{"invalid email", func(p *auth_models.SignUpInput) { p.Email = "not-an-email" }},
		{"invalid phone", func(p *auth_models.SignUpInput) { p.Phone = "0612345678" }},
		{"password too short", func(p *auth_models.SignUpInput) { p.PasswordHash = "1234567" }},
		{"password whitespace", func(p *auth_models.SignUpInput) { p.PasswordHash = "        " }},
		{"first name > 50 runes", func(p *auth_models.SignUpInput) { p.FirstName = strings.Repeat("é", 51) }},
		{"birthdate invalid format", func(p *auth_models.SignUpInput) { p.Birthdate = "19950515" }},
		{"birthdate under 13", func(p *auth_models.SignUpInput) { p.Birthdate = "01012022" }},
		{"birthdate over 120", func(p *auth_models.SignUpInput) { p.Birthdate = "01011890" }},
		{"invalid gender", func(p *auth_models.SignUpInput) { p.Gender = 3 }},
		{"bio > 500 runes", func(p *auth_models.SignUpInput) { p.Bio = strings.Repeat("é", 501) }},
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

func TestAuthInputsAndPayloads_Validate(t *testing.T) {
	t.Run("LoginInput", func(t *testing.T) {
		var nilL *auth_models.LoginInput
		assertBadRequest(t, nilL.Validate())
		require.NoError(t, (&auth_models.LoginInput{
			Email:                  "alice@numan.app",
			PasswordHash:           "hash",
			FirebaseInstallationID: "fb-1",
		}).Validate())
		assertBadRequest(t, (&auth_models.LoginInput{Email: "bad", PasswordHash: "hash", FirebaseInstallationID: "fb-1"}).Validate())
		assertBadRequest(t, (&auth_models.LoginInput{Email: "alice@numan.app", PasswordHash: " ", FirebaseInstallationID: "fb-1"}).Validate())
		assertBadRequest(t, (&auth_models.LoginInput{Email: "alice@numan.app", PasswordHash: "hash", FirebaseInstallationID: ""}).Validate())
	})

	t.Run("DeleteSessionInput", func(t *testing.T) {
		var nilD *auth_models.DeleteSessionInput
		assertBadRequest(t, nilD.Validate())
		require.NoError(t, (&auth_models.DeleteSessionInput{SessionID: 1}).Validate())
		assertBadRequest(t, (&auth_models.DeleteSessionInput{SessionID: 0}).Validate())
	})

	t.Run("AddCertificationInput", func(t *testing.T) {
		var nilC *auth_models.AddCertificationInput
		assertBadRequest(t, nilC.Validate())
		require.NoError(t, (&auth_models.AddCertificationInput{Prestataire: "federation_francaise", CertificationID: "LIC-12345"}).Validate())
		assertBadRequest(t, (&auth_models.AddCertificationInput{Prestataire: "a", CertificationID: "LIC-12345"}).Validate())
		assertBadRequest(t, (&auth_models.AddCertificationInput{Prestataire: "federation_francaise", CertificationID: "ab"}).Validate())
		assertBadRequest(t, (&auth_models.AddCertificationInput{Prestataire: "federation_francaise", CertificationID: "LIC 12345!"}).Validate())
	})

	t.Run("SessionsPayload", func(t *testing.T) {
		var nilS *auth_models.SessionsPayload
		assertBadRequest(t, nilS.Validate())
		s := auth_models.SessionsPayload{
			ID:                     1,
			UserID:                 2,
			MasterToken:            "master",
			FirebaseInstallationID: "fb-1",
			ExpiresAt:              1000,
		}
		require.NoError(t, s.Validate())
		sBad := s
		sBad.ExpiresAt = 0
		assertBadRequest(t, sBad.Validate())
	})

	t.Run("PreAuthSession", func(t *testing.T) {
		var nilP *auth_models.PreAuthSession
		assertBadRequest(t, nilP.Validate())
		require.NoError(t, (&auth_models.PreAuthSession{
			Token:            "tok",
			Flow:             "signup",
			UserID:           0,
			Method:           variables.TwoFactorMethodEmail,
			AvailableMethods: []int{variables.TwoFactorMethodEmail},
		}).Validate())
		require.NoError(t, (&auth_models.PreAuthSession{
			Token:            "tok",
			Flow:             "login",
			UserID:           10,
			Method:           variables.TwoFactorBitEmail | variables.TwoFactorBitTOTP,
			AvailableMethods: []int{variables.TwoFactorMethodEmail, variables.TwoFactorMethodTOTP},
		}).Validate())
		assertBadRequest(t, (&auth_models.PreAuthSession{Token: "tok", Flow: "login", UserID: 0}).Validate())
		assertBadRequest(t, (&auth_models.PreAuthSession{Token: "tok", Flow: "unknown", UserID: 1}).Validate())
		assertBadRequest(t, (&auth_models.PreAuthSession{Token: "tok", Flow: "setup", UserID: 1, AvailableMethods: []int{99}}).Validate())
		assertBadRequest(t, (&auth_models.PreAuthSession{Token: "tok", Flow: "setup", UserID: 1, Attempts: -1}).Validate())
	})

	t.Run("UserPayload", func(t *testing.T) {
		var nilU *auth_models.UserPayload
		assertBadRequest(t, nilU.Validate())
		u := auth_models.UserPayload{
			ID:                  1,
			Username:            "alice",
			Email:               "alice@numan.app",
			PasswordHash:        "hash",
			TrustScore:          0.5,
			ModeratorTrustScore: 0.5,
		}
		require.NoError(t, u.Validate())

		uBadTrust := u
		uBadTrust.TrustScore = math.NaN()
		assertBadRequest(t, uBadTrust.Validate())

		uBadGrade := u
		uBadGrade.Grade = 99
		assertBadRequest(t, uBadGrade.Validate())

		uBad2FA := u
		uBad2FA.TwoFactorMethod = 8
		assertBadRequest(t, uBad2FA.Validate())
	})
}
