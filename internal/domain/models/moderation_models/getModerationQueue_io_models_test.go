package moderation_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetModerationQueueInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

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

	for _, qt := range []string{
		variables.ModerationQueueActive,
		variables.ModerationQueuePeerReview,
		variables.ModerationQueueFinalVote,
		variables.ModerationQueueAdmin,
	} {
		assert.NoError(t, (&moderation_models.GetModerationQueueInput{Limit: 100, QueueType: qt}).Validate())
	}
}
