package report_models_test

import (
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/report_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportPayload_ValidateAndOriginLabel(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	assert.Equal(t, variables.ReportOriginLabelSystem, report_models.ResolveOriginLabel(variables.ReportOriginSystem))
	assert.Equal(t, variables.ReportOriginLabelAutoAI, report_models.ResolveOriginLabel(variables.ReportOriginAutoAI))
	assert.Equal(t, variables.ReportOriginLabelHybrid, report_models.ResolveOriginLabel(variables.ReportOriginHybrid))
	assert.Equal(t, variables.ReportOriginLabelUser, report_models.ResolveOriginLabel(variables.ReportOriginUser))

	var nilPayload *report_models.ReportPayload
	assertBadRequest(t, nilPayload.Validate())

	valid := report_models.ReportPayload{
		ID:               9001,
		ReporterID:       12,
		ReportOrigin:     variables.ReportOriginUser,
		TargetType:       variables.ReportTargetPost,
		TargetIDs:        []int64{500},
		TargetOwnerID:    88,
		AuthorTrustScore: 0.4,
		IAScoreMax:       0.2,
		Category:         variables.ReportCatUnderage,
		State:            variables.ReportStatePending,
		Importance:       math.Inf(1), // +Inf allowed for underage/self-harm
		CreatedAt:        1700000000000,
		UpdatedAt:        1700000000000,
	}
	assert.NoError(t, valid.Validate())

	// AutoAI with ReporterID = 0 and TargetOwnerID = 0 allowed
	autoAI := valid
	autoAI.ReporterID = 0
	autoAI.TargetOwnerID = 0
	autoAI.ReportOrigin = variables.ReportOriginAutoAI
	autoAI.Importance = 5.5
	assert.NoError(t, autoAI.Validate())

	cases := []struct {
		name   string
		mutate func(*report_models.ReportPayload)
	}{
		{"invalid_id", func(p *report_models.ReportPayload) { p.ID = 0 }},
		{"user_origin_missing_reporter", func(p *report_models.ReportPayload) { p.ReporterID = 0 }},
		{"invalid_origin", func(p *report_models.ReportPayload) { p.ReportOrigin = 9 }},
		{"invalid_target_type", func(p *report_models.ReportPayload) { p.TargetType = 99 }},
		{"empty_target_ids", func(p *report_models.ReportPayload) { p.TargetIDs = []int64{} }},
		{"bad_target_id", func(p *report_models.ReportPayload) { p.TargetIDs = []int64{-5} }},
		{"neg_target_owner", func(p *report_models.ReportPayload) { p.TargetOwnerID = -1 }},
		{"invalid_category", func(p *report_models.ReportPayload) { p.Category = 55 }},
		{"invalid_state", func(p *report_models.ReportPayload) { p.State = 9 }},
		{"neg_report_count", func(p *report_models.ReportPayload) { p.ReportCount = -1 }},
		{"trust_gt_1", func(p *report_models.ReportPayload) { p.AuthorTrustScore = 1.1 }},
		{"trust_nan", func(p *report_models.ReportPayload) { p.AuthorTrustScore = math.NaN() }},
		{"ia_lt_0", func(p *report_models.ReportPayload) { p.IAScoreMax = -0.1 }},
		{"acc_inf", func(p *report_models.ReportPayload) { p.AccumulatedReportScore = math.Inf(1) }},
		{"importance_nan", func(p *report_models.ReportPayload) { p.Importance = math.NaN() }},
		{"importance_neg_inf", func(p *report_models.ReportPayload) { p.Importance = math.Inf(-1) }},
		{"importance_neg", func(p *report_models.ReportPayload) { p.Importance = -1 }},
		{"reason_too_long", func(p *report_models.ReportPayload) { p.Reason = strings.Repeat("a", 1001) }},
		{"rationale_too_long", func(p *report_models.ReportPayload) { p.Rationale = strings.Repeat("b", 2001) }},
		{"neg_created_at", func(p *report_models.ReportPayload) { p.CreatedAt = -1 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := valid
			tc.mutate(&p)
			assertBadRequest(t, p.Validate())
		})
	}
}

func TestReportValidators(t *testing.T) {
	assert.True(t, report_models.IsValidReportTargetType(variables.ReportTargetPost))
	assert.True(t, report_models.IsValidReportTargetType(variables.ReportTargetComment))
	assert.True(t, report_models.IsValidReportTargetType(variables.ReportTargetConversation))
	assert.True(t, report_models.IsValidReportTargetType(variables.ReportTargetMessage))
	assert.True(t, report_models.IsValidReportTargetType(variables.ReportTargetUser))
	assert.False(t, report_models.IsValidReportTargetType(4))
	assert.False(t, report_models.IsValidReportTargetType(-1))

	assert.True(t, report_models.IsValidReportCategory(variables.ReportCatSpam))
	assert.True(t, report_models.IsValidReportCategory(variables.ReportCatHarassmentMoral))
	assert.True(t, report_models.IsValidReportCategory(variables.ReportCatHarassmentSexual))
	assert.True(t, report_models.IsValidReportCategory(variables.ReportCatHateSpeech))
	assert.True(t, report_models.IsValidReportCategory(variables.ReportCatIdentityTheft))
	assert.True(t, report_models.IsValidReportCategory(variables.ReportCatUnderage))
	assert.True(t, report_models.IsValidReportCategory(variables.ReportCatNonConsensual))
	assert.True(t, report_models.IsValidReportCategory(variables.ReportCatIllegalContent))
	assert.True(t, report_models.IsValidReportCategory(variables.ReportCatSelfHarm))
	assert.True(t, report_models.IsValidReportCategory(variables.ReportCatAIPornography))
	assert.True(t, report_models.IsValidReportCategory(variables.ReportCatOther))
	assert.False(t, report_models.IsValidReportCategory(0))
	assert.False(t, report_models.IsValidReportCategory(11))

	assert.True(t, report_models.IsValidReportOrigin(variables.ReportOriginUser))
	assert.True(t, report_models.IsValidReportOrigin(variables.ReportOriginAutoAI))
	assert.True(t, report_models.IsValidReportOrigin(variables.ReportOriginHybrid))
	assert.True(t, report_models.IsValidReportOrigin(variables.ReportOriginSystem))
	assert.False(t, report_models.IsValidReportOrigin(9))

	assert.True(t, report_models.IsValidReportState(variables.ReportStatePending))
	assert.True(t, report_models.IsValidReportState(variables.ReportStateInProgress))
	assert.True(t, report_models.IsValidReportState(variables.ReportStateWaitingForInfo))
	assert.True(t, report_models.IsValidReportState(variables.ReportStatePendingPeerReview))
	assert.True(t, report_models.IsValidReportState(variables.ReportStateEscalated))
	assert.True(t, report_models.IsValidReportState(variables.ReportStatePendingFinalVote))
	assert.True(t, report_models.IsValidReportState(variables.ReportStateClosed))
	assert.False(t, report_models.IsValidReportState(99))
}
