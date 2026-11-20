package moderation_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsValidTrustScoreEventType(t *testing.T) {
	validTypes := []string{
		"quarantine_approved",
		"  QUARANTINE_APPROVED  ",
		variables.TrustEventQuarantineApproved,
		variables.TrustTriggerVerifiedIdentity,
		variables.TrustTriggerSuccessfulAppeal,
		variables.TrustTriggerMonthlyClean,
		variables.TrustTriggerLikedByAdmin,
		variables.TrustTriggerJustifiedReport,
		variables.TrustTriggerModBatchLike,
		variables.TrustTriggerSpamDetectedAuto,
		variables.TrustTriggerMutedInPublicComm,
		variables.TrustTriggerAbusiveReport,
		variables.TrustTriggerMinorInfraction,
		variables.TrustTriggerMajorInfraction,
	}

	for _, eventType := range validTypes {
		assert.True(t, moderation_models.IsValidTrustScoreEventType(eventType), "expected valid event type: %q", eventType)
	}

	assert.False(t, moderation_models.IsValidTrustScoreEventType(""))
	assert.False(t, moderation_models.IsValidTrustScoreEventType("UNKNOWN_TYPE"))
}

func TestTrustScoreEventPayload_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

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
	assertBadRequest(t, (&moderation_models.TrustScoreEventPayload{EventID: 1, UserID: -1, EventType: variables.TrustTriggerMonthlyClean}).Validate())
	assertBadRequest(t, (&moderation_models.TrustScoreEventPayload{EventID: 1, UserIDs: make([]int64, 501), EventType: variables.TrustTriggerModBatchLike}).Validate())
	assertBadRequest(t, (&moderation_models.TrustScoreEventPayload{EventID: 1, UserID: 10, TargetID: -1, EventType: variables.TrustTriggerMonthlyClean}).Validate())
	assertBadRequest(t, (&moderation_models.TrustScoreEventPayload{EventID: 1, UserID: 10, Reason: strings.Repeat("r", 1001), EventType: variables.TrustTriggerMonthlyClean}).Validate())
	assertBadRequest(t, (&moderation_models.TrustScoreEventPayload{EventID: 1, UserID: 10, OccurredAtMs: -1, EventType: variables.TrustTriggerMonthlyClean}).Validate())

	assert.NoError(t, (&moderation_models.TrustScoreEventPayload{
		EventID:      2,
		UserIDs:      []int64{1, 2},
		EventType:    variables.TrustTriggerModBatchLike,
		TargetID:     99,
		Reason:       "Batch like",
		OccurredAtMs: 123456789,
	}).Validate())
}
