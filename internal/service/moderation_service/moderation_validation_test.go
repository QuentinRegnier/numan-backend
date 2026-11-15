package moderation_service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertModBadRequest(t *testing.T, err error) {
	t.Helper()
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestModerationServices_ValidationGuardsBeforeRedis(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	// 1. ModBatchLikeService
	_, err := ModBatchLikeService(ctx, 0, moderation_models.ModBatchLikeInput{AuthorUserIDs: []int64{1}})
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)

	_, err = ModBatchLikeService(ctx, 10, moderation_models.ModBatchLikeInput{AuthorUserIDs: nil})
	assertModBadRequest(t, err)
	_, err = ModBatchLikeService(ctx, 10, moderation_models.ModBatchLikeInput{AuthorUserIDs: []int64{1, -2}})
	assertModBadRequest(t, err)
	_, err = ModBatchLikeService(ctx, 10, moderation_models.ModBatchLikeInput{
		AuthorUserIDs: []int64{1},
		Reason:        strings.Repeat("r", 501),
	})
	assertModBadRequest(t, err)

	// 2. ReviewQuarantineMedia
	_, err = ReviewQuarantineMedia(ctx, 10, moderation_models.ReviewQuarantineInput{MediaID: 0, Decision: "approve"})
	assertModBadRequest(t, err)
	_, err = ReviewQuarantineMedia(ctx, 10, moderation_models.ReviewQuarantineInput{MediaID: 1, PostID: -1, Decision: "approve"})
	assertModBadRequest(t, err)
	_, err = ReviewQuarantineMedia(ctx, 10, moderation_models.ReviewQuarantineInput{MediaID: 1, Decision: "unknown"})
	assertModBadRequest(t, err)
	_, err = ReviewQuarantineMedia(ctx, 10, moderation_models.ReviewQuarantineInput{
		MediaID:  1,
		Decision: "approve",
		Reason:   strings.Repeat("x", 501),
	})
	assertModBadRequest(t, err)

	// 3. GetModerationQueue
	_, err = GetModerationQueue(ctx, 10, moderation_models.GetModerationQueueInput{Limit: -1})
	assertModBadRequest(t, err)
	_, err = GetModerationQueue(ctx, 10, moderation_models.GetModerationQueueInput{Limit: 101})
	assertModBadRequest(t, err)
	_, err = GetModerationQueue(ctx, 10, moderation_models.GetModerationQueueInput{Offset: -1})
	assertModBadRequest(t, err)
	_, err = GetModerationQueue(ctx, 10, moderation_models.GetModerationQueueInput{QueueType: "invalid"})
	assertModBadRequest(t, err)

	// 4. Ticket lifecycle services
	_, err = ClaimTicketService(ctx, 10, 0)
	assertModBadRequest(t, err)
	_, err = ReleaseTicketService(ctx, 10, variables.UserGradeModerator, -1)
	assertModBadRequest(t, err)

	_, err = RequestTicketInfoService(ctx, 10, variables.UserGradeModerator, moderation_models.RequestTicketInfoInput{
		ReportID: 0,
		Question: "Question valide ?",
	})
	assertModBadRequest(t, err)
	_, err = RequestTicketInfoService(ctx, 10, variables.UserGradeModerator, moderation_models.RequestTicketInfoInput{
		ReportID: 1,
		Question: "no",
	})
	assertModBadRequest(t, err)

	_, err = ReplyTicketInfoService(ctx, 10, variables.UserGradeUser, moderation_models.ReplyTicketInfoInput{
		ReportID: 0,
		Reply:    "Réponse valide",
	})
	assertModBadRequest(t, err)
	_, err = ReplyTicketInfoService(ctx, 10, variables.UserGradeUser, moderation_models.ReplyTicketInfoInput{
		ReportID: 1,
		Reply:    "   ",
	})
	assertModBadRequest(t, err)

	_, err = ProposeBlameService(ctx, 10, variables.UserGradeModerator, moderation_models.ProposeBlameInput{
		ReportID:   0,
		Motivation: "Motivation valide",
	})
	assertModBadRequest(t, err)
	_, err = ProposeBlameService(ctx, 10, variables.UserGradeModerator, moderation_models.ProposeBlameInput{
		ReportID:   1,
		Motivation: "abc",
	})
	assertModBadRequest(t, err)

	_, err = ReviewBlameProposalService(ctx, 10, variables.UserGradeModerator, moderation_models.ReviewBlameInput{
		ReportID:  1,
		Decision:  "bad",
		Rationale: "Justification valide",
	})
	assertModBadRequest(t, err)
	_, err = ReviewBlameProposalService(ctx, 10, variables.UserGradeModerator, moderation_models.ReviewBlameInput{
		ReportID:  1,
		Decision:  "validate",
		Rationale: "no",
	})
	assertModBadRequest(t, err)

	_, err = ResolveFinalVoteService(ctx, 10, variables.UserGradeModerator, moderation_models.FinalVoteBlameInput{
		ReportID:  1,
		Decision:  "bad",
		Rationale: "Justification valide",
	})
	assertModBadRequest(t, err)
	_, err = ResolveFinalVoteService(ctx, 10, variables.UserGradeModerator, moderation_models.FinalVoteBlameInput{
		ReportID:  1,
		Decision:  "confirm_blame",
		Rationale: "no",
	})
	assertModBadRequest(t, err)

	_, err = EscalateTicketService(ctx, 10, variables.UserGradeModerator, moderation_models.EscalateTicketInput{
		ReportID:       1,
		EscalationType: "invalid",
		Reason:         "Raison valide",
	})
	assertModBadRequest(t, err)
	_, err = EscalateTicketService(ctx, 10, variables.UserGradeModerator, moderation_models.EscalateTicketInput{
		ReportID:       1,
		EscalationType: "hard_ban",
		Reason:         "no",
	})
	assertModBadRequest(t, err)

	_, err = CloseTicketService(ctx, 10, true, "Motif valide", 0)
	assertModBadRequest(t, err)
	_, err = CloseTicketService(ctx, 10, true, strings.Repeat("z", 1001), 1)
	assertModBadRequest(t, err)

	// 5. PublishTrustScoreEvent
	err = PublishTrustScoreEvent(ctx, 42, "UNKNOWN_EVENT", 10, "motif")
	assertModBadRequest(t, err)

	keys, errKeys := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, errKeys)
	assert.Empty(t, keys, "No invalid moderation call should write to Redis")
}
