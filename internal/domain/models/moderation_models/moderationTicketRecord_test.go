package moderation_models_test

import (
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModerationTicketRecord_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	assertBadRequest(t, (*moderation_models.ModerationTicketRecord)(nil).Validate())

	validTicket := moderation_models.ModerationTicketRecord{
		ReportID:        100,
		TargetType:      variables.ReportTargetPost,
		State:           variables.ReportStatePending,
		StaticBaseScore: math.Inf(1),
		PriorityScore:   math.Inf(1),
	}
	assert.NoError(t, validTicket.Validate())

	badTicket := validTicket
	badTicket.PriorityScore = math.NaN()
	assertBadRequest(t, badTicket.Validate())

	cases := []struct {
		name   string
		mutate func(*moderation_models.ModerationTicketRecord)
	}{
		{"zero report id", func(r *moderation_models.ModerationTicketRecord) { r.ReportID = 0 }},
		{"invalid target type", func(r *moderation_models.ModerationTicketRecord) { r.TargetType = 99 }},
		{"negative target id", func(r *moderation_models.ModerationTicketRecord) { r.TargetID = -1 }},
		{"negative target owner id", func(r *moderation_models.ModerationTicketRecord) { r.TargetOwnerID = -1 }},
		{"invalid category", func(r *moderation_models.ModerationTicketRecord) { r.Category = 999 }},
		{"invalid origin", func(r *moderation_models.ModerationTicketRecord) { r.ReportOrigin = 99 }},
		{"invalid state", func(r *moderation_models.ModerationTicketRecord) { r.State = 99 }},
		{"nan static base score", func(r *moderation_models.ModerationTicketRecord) { r.StaticBaseScore = math.NaN() }},
		{"negative static base score", func(r *moderation_models.ModerationTicketRecord) { r.StaticBaseScore = -1 }},
		{"neg inf priority score", func(r *moderation_models.ModerationTicketRecord) { r.PriorityScore = math.Inf(-1) }},
		{"invalid reporter id", func(r *moderation_models.ModerationTicketRecord) { r.ReporterIDs = []int64{1, 0} }},
		{"negative claimed mod", func(r *moderation_models.ModerationTicketRecord) { r.ClaimedByModID = -1 }},
		{"negative proposer mod", func(r *moderation_models.ModerationTicketRecord) { r.ProposerModID = -1 }},
		{"negative reviewer mod", func(r *moderation_models.ModerationTicketRecord) { r.ReviewerModID = -1 }},
		{"negative final voter mod", func(r *moderation_models.ModerationTicketRecord) { r.FinalVoterModID = -1 }},
		{"negative lock expires", func(r *moderation_models.ModerationTicketRecord) { r.LockExpiresAtMs = -1 }},
		{"negative waiting until", func(r *moderation_models.ModerationTicketRecord) { r.WaitingUntilMs = -1 }},
		{"negative created at", func(r *moderation_models.ModerationTicketRecord) { r.CreatedAt = -1 }},
		{"negative updated at", func(r *moderation_models.ModerationTicketRecord) { r.UpdatedAt = -1 }},
		{"reason too long", func(r *moderation_models.ModerationTicketRecord) { r.Reason = strings.Repeat("x", 1001) }},
		{"blame motivation too long", func(r *moderation_models.ModerationTicketRecord) { r.BlameMotivation = strings.Repeat("x", 1001) }},
		{"reviewer rationale too long", func(r *moderation_models.ModerationTicketRecord) { r.ReviewerRationale = strings.Repeat("x", 1001) }},
		{"escalation reason too long", func(r *moderation_models.ModerationTicketRecord) { r.EscalationReason = strings.Repeat("x", 1001) }},
		{"cgu feedback note too long", func(r *moderation_models.ModerationTicketRecord) { r.CGUFeedbackNote = strings.Repeat("x", 2001) }},
		{"invalid audit blame", func(r *moderation_models.ModerationTicketRecord) {
			r.AuditBlameRecords = []moderation_models.BlameRecord{{TargetType: variables.ReportTargetPost}}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := validTicket
			tc.mutate(&rec)
			assertBadRequest(t, rec.Validate())
		})
	}
}
