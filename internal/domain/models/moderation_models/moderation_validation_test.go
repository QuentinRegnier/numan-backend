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

func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestTicketActionInputs_Validate(t *testing.T) {
	// ClaimTicketInput & ReleaseTicketInput
	assertBadRequest(t, (*moderation_models.ClaimTicketInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.ClaimTicketInput{ReportID: 0}).Validate())
	assert.NoError(t, (&moderation_models.ClaimTicketInput{ReportID: 1}).Validate())

	assertBadRequest(t, (*moderation_models.ReleaseTicketInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.ReleaseTicketInput{ReportID: -2}).Validate())
	assert.NoError(t, (&moderation_models.ReleaseTicketInput{ReportID: 1}).Validate())

	// CloseTicketInput
	assertBadRequest(t, (*moderation_models.CloseTicketInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.CloseTicketInput{ReportID: 0, Rationale: "valid rationale"}).Validate())
	assertBadRequest(t, (&moderation_models.CloseTicketInput{ReportID: 1, Rationale: "ab"}).Validate())
	assertBadRequest(t, (&moderation_models.CloseTicketInput{ReportID: 1, Rationale: strings.Repeat("x", 1001)}).Validate())
	assert.NoError(t, (&moderation_models.CloseTicketInput{ReportID: 1, IsDismiss: true, Rationale: "Signalement clos"}).Validate())

	// EscalateTicketInput
	assertBadRequest(t, (*moderation_models.EscalateTicketInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.EscalateTicketInput{ReportID: 0, EscalationType: "hard_ban_request", Reason: "Raison valide"}).Validate())
	assertBadRequest(t, (&moderation_models.EscalateTicketInput{ReportID: 1, EscalationType: "unknown", Reason: "Raison valide"}).Validate())
	assertBadRequest(t, (&moderation_models.EscalateTicketInput{ReportID: 1, EscalationType: "cgu_gray_area", Reason: "abc"}).Validate())
	assertBadRequest(t, (&moderation_models.EscalateTicketInput{ReportID: 1, EscalationType: "cgu_gray_area", Reason: "Raison valide", CGUFeedbackNote: strings.Repeat("n", 2001)}).Validate())
	assert.NoError(t, (&moderation_models.EscalateTicketInput{ReportID: 1, EscalationType: "hard_ban_request", Reason: "Contenu gravement illicite"}).Validate())

	// ProposeBlameInput
	assertBadRequest(t, (*moderation_models.ProposeBlameInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.ProposeBlameInput{ReportID: 0, Motivation: "Motivation valide"}).Validate())
	assertBadRequest(t, (&moderation_models.ProposeBlameInput{ReportID: 1, Motivation: "  no  "}).Validate())
	assert.NoError(t, (&moderation_models.ProposeBlameInput{ReportID: 1, Motivation: "Infraction caractérisée"}).Validate())

	// ReviewBlameInput
	assertBadRequest(t, (*moderation_models.ReviewBlameInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.ReviewBlameInput{ReportID: 0, Decision: "validate", Rationale: "Motif valide"}).Validate())
	assertBadRequest(t, (&moderation_models.ReviewBlameInput{ReportID: 1, Decision: "invalid", Rationale: "Motif valide"}).Validate())
	assertBadRequest(t, (&moderation_models.ReviewBlameInput{ReportID: 1, Decision: "oppose", Rationale: "ab"}).Validate())
	assert.NoError(t, (&moderation_models.ReviewBlameInput{ReportID: 1, Decision: "validate", Rationale: "Consensus confirmé"}).Validate())

	// FinalVoteBlameInput
	assertBadRequest(t, (*moderation_models.FinalVoteBlameInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.FinalVoteBlameInput{ReportID: 0, Decision: "confirm_blame", Rationale: "Motif valide"}).Validate())
	assertBadRequest(t, (&moderation_models.FinalVoteBlameInput{ReportID: 1, Decision: "other", Rationale: "Motif valide"}).Validate())
	assertBadRequest(t, (&moderation_models.FinalVoteBlameInput{ReportID: 1, Decision: "reject_blame", Rationale: "ok"}).Validate())
	assert.NoError(t, (&moderation_models.FinalVoteBlameInput{ReportID: 1, Decision: "confirm_blame", Rationale: "Arbitrage confirmé"}).Validate())

	// RequestTicketInfoInput & ReplyTicketInfoInput
	assertBadRequest(t, (*moderation_models.RequestTicketInfoInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.RequestTicketInfoInput{ReportID: 1, Question: "  "}).Validate())
	assert.NoError(t, (&moderation_models.RequestTicketInfoInput{ReportID: 1, Question: "Précisions ?"}).Validate())

	assertBadRequest(t, (*moderation_models.ReplyTicketInfoInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.ReplyTicketInfoInput{ReportID: 1, Reply: "   "}).Validate())
	assertBadRequest(t, (&moderation_models.ReplyTicketInfoInput{ReportID: 1, Reply: strings.Repeat("a", 2001)}).Validate())
	assert.NoError(t, (&moderation_models.ReplyTicketInfoInput{ReportID: 1, Reply: "Voici le contexte"}).Validate())
}

func TestModBatchLikeReviewQuarantineAndQueueInputs_Validate(t *testing.T) {
	// ModBatchLikeInput
	assertBadRequest(t, (*moderation_models.ModBatchLikeInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.ModBatchLikeInput{AuthorUserIDs: nil}).Validate())
	assertBadRequest(t, (&moderation_models.ModBatchLikeInput{AuthorUserIDs: []int64{1, -5}}).Validate())
	assertBadRequest(t, (&moderation_models.ModBatchLikeInput{AuthorUserIDs: []int64{1}, Reason: strings.Repeat("r", 501)}).Validate())
	assert.NoError(t, (&moderation_models.ModBatchLikeInput{AuthorUserIDs: []int64{1, 2}, Reason: "Qualité"}).Validate())

	// ReviewQuarantineInput
	assertBadRequest(t, (*moderation_models.ReviewQuarantineInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.ReviewQuarantineInput{MediaID: 0, Decision: "approve"}).Validate())
	assertBadRequest(t, (&moderation_models.ReviewQuarantineInput{MediaID: 1, PostID: -1, Decision: "approve"}).Validate())
	assertBadRequest(t, (&moderation_models.ReviewQuarantineInput{MediaID: 1, Decision: "unknown"}).Validate())
	assertBadRequest(t, (&moderation_models.ReviewQuarantineInput{MediaID: 1, Decision: "reject_minor", Reason: strings.Repeat("x", 501)}).Validate())
	assert.NoError(t, (&moderation_models.ReviewQuarantineInput{MediaID: 10, PostID: 0, Decision: "reject_major", Reason: "Illégal"}).Validate())

	// GetModerationQueueInput
	assertBadRequest(t, (*moderation_models.GetModerationQueueInput)(nil).Validate())
	assert.NoError(t, (&moderation_models.GetModerationQueueInput{Limit: 0, Offset: 0}).Validate())
	assertBadRequest(t, (&moderation_models.GetModerationQueueInput{Limit: -1}).Validate())
	assertBadRequest(t, (&moderation_models.GetModerationQueueInput{Limit: 101}).Validate())
	assertBadRequest(t, (&moderation_models.GetModerationQueueInput{Offset: -1}).Validate())
	assertBadRequest(t, (&moderation_models.GetModerationQueueInput{QueueType: "invalid_queue"}).Validate())
	badOrigin := 99
	assertBadRequest(t, (&moderation_models.GetModerationQueueInput{OriginFilter: &badOrigin}).Validate())
	badTarget := 4
	assertBadRequest(t, (&moderation_models.GetModerationQueueInput{TargetTypeFilter: &badTarget}).Validate())
	validOrigin := variables.ReportOriginAutoAI
	validTarget := variables.ReportTargetPost
	assert.NoError(t, (&moderation_models.GetModerationQueueInput{
		Limit:            25,
		Offset:           0,
		QueueType:        variables.ModerationQueueAdmin,
		OriginFilter:     &validOrigin,
		TargetTypeFilter: &validTarget,
	}).Validate())
}

func TestTrustScoreEventAndInternalRecords_Validate(t *testing.T) {
	// TrustScoreEventPayload
	assertBadRequest(t, (*moderation_models.TrustScoreEventPayload)(nil).Validate())
	validEvt := moderation_models.TrustScoreEventPayload{
		EventID:   1,
		UserID:    10,
		EventType: variables.TrustEventQuarantineApproved,
	}
	assert.NoError(t, validEvt.Validate())
	assertBadRequest(t, (&moderation_models.TrustScoreEventPayload{EventID: 0, UserID: 10, EventType: variables.TrustTriggerMonthlyClean}).Validate())
	assertBadRequest(t, (&moderation_models.TrustScoreEventPayload{EventID: 1, UserID: 10, EventType: "UNKNOWN_TYPE"}).Validate())
	assertBadRequest(t, (&moderation_models.TrustScoreEventPayload{EventID: 1, EventType: variables.TrustTriggerModBatchLike}).Validate())
	assertBadRequest(t, (&moderation_models.TrustScoreEventPayload{EventID: 1, UserIDs: []int64{1, 0}, EventType: variables.TrustTriggerModBatchLike}).Validate())

	// BlameRecord
	assertBadRequest(t, (*moderation_models.BlameRecord)(nil).Validate())
	assertBadRequest(t, (&moderation_models.BlameRecord{TargetType: variables.ReportTargetPost}).Validate())
	assert.NoError(t, (&moderation_models.BlameRecord{ProposerModID: 1, ValidatorModID: 2}).Validate())

	// ModerationTicketRecord
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

	// ModeratorStatsRecord
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
}
