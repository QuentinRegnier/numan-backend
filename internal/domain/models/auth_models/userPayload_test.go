package auth_models_test

import (
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/badge_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPayload_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilU *auth_models.UserPayload
	assertBadRequest(t, nilU.Validate())

	valid := func() auth_models.UserPayload {
		return auth_models.UserPayload{
			ID:                  1,
			Username:            "alice",
			Email:               "alice@numan.app",
			PasswordHash:        "hash",
			TrustScore:          0.5,
			ModeratorTrustScore: 0.5,
		}
	}

	u := valid()
	require.NoError(t, u.Validate())

	uWithBadges := valid()
	uWithBadges.Badges = auth_models.UserBadges{
		"verified": badge_models.BadgeMetadata{IssuedAt: time.Now().UTC()},
	}
	require.NoError(t, uWithBadges.Validate())

	cases := []struct {
		name   string
		mutate func(p *auth_models.UserPayload)
	}{
		{"non-positive id", func(p *auth_models.UserPayload) { p.ID = 0 }},
		{"blank username", func(p *auth_models.UserPayload) { p.Username = " " }},
		{"blank email", func(p *auth_models.UserPayload) { p.Email = " " }},
		{"blank password_hash", func(p *auth_models.UserPayload) { p.PasswordHash = " " }},
		{"negative profile_picture_id", func(p *auth_models.UserPayload) { p.ProfilePictureID = -1 }},
		{"invalid sex", func(p *auth_models.UserPayload) { p.Sex = 3 }},
		{"invalid grade", func(p *auth_models.UserPayload) { p.Grade = 99 }},
		{"NaN trust_score", func(p *auth_models.UserPayload) { p.TrustScore = math.NaN() }},
		{"out of range trust_score", func(p *auth_models.UserPayload) { p.TrustScore = 1.5 }},
		{"NaN moderator_trust_score", func(p *auth_models.UserPayload) { p.ModeratorTrustScore = math.NaN() }},
		{"out of range moderator_trust_score", func(p *auth_models.UserPayload) { p.ModeratorTrustScore = -0.1 }},
		{"negative blame_count", func(p *auth_models.UserPayload) { p.BlameCount = -1 }},
		{"negative quarantine_posts_approved", func(p *auth_models.UserPayload) { p.QuarantinePostsApproved = -1 }},
		{"invalid two_factor_method", func(p *auth_models.UserPayload) { p.TwoFactorMethod = 8 }},
		{"invalid badges", func(p *auth_models.UserPayload) {
			p.Badges = auth_models.UserBadges{"": badge_models.BadgeMetadata{IssuedAt: time.Now().UTC()}}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := valid()
			tc.mutate(&p)
			assertBadRequest(t, p.Validate())
		})
	}
}
