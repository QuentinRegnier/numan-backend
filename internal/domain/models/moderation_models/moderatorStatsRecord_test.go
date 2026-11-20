package moderation_models_test

import (
	"math"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModeratorStatsRecord_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	assertBadRequest(t, (*moderation_models.ModeratorStatsRecord)(nil).Validate())

	validStats := moderation_models.ModeratorStatsRecord{
		UserID:              10,
		ModeratorTrustScore: 0.85,
		Grade:               variables.UserGradeModerator,
		BlameCount:          1,
		BlameHistory:        []moderation_models.BlameRecord{{ProposerModID: 1, ValidatorModID: 2}},
	}
	assert.NoError(t, validStats.Validate())

	badStats := validStats
	badStats.ModeratorTrustScore = 1.5
	assertBadRequest(t, badStats.Validate())

	cases := []struct {
		name   string
		mutate func(*moderation_models.ModeratorStatsRecord)
	}{
		{"zero user id", func(s *moderation_models.ModeratorStatsRecord) { s.UserID = 0 }},
		{"nan trust score", func(s *moderation_models.ModeratorStatsRecord) { s.ModeratorTrustScore = math.NaN() }},
		{"inf trust score", func(s *moderation_models.ModeratorStatsRecord) { s.ModeratorTrustScore = math.Inf(1) }},
		{"negative trust score", func(s *moderation_models.ModeratorStatsRecord) { s.ModeratorTrustScore = -0.1 }},
		{"invalid grade", func(s *moderation_models.ModeratorStatsRecord) { s.Grade = 99 }},
		{"negative blame count", func(s *moderation_models.ModeratorStatsRecord) { s.BlameCount = -1 }},
		{"negative updated at", func(s *moderation_models.ModeratorStatsRecord) { s.UpdatedAt = -1 }},
		{"invalid blame history", func(s *moderation_models.ModeratorStatsRecord) {
			s.BlameHistory = []moderation_models.BlameRecord{{TargetType: variables.ReportTargetPost}}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := validStats
			tc.mutate(&rec)
			assertBadRequest(t, rec.Validate())
		})
	}
}
