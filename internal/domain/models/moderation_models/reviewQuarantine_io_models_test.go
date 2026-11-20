package moderation_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReviewQuarantineInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	assertBadRequest(t, (*moderation_models.ReviewQuarantineInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.ReviewQuarantineInput{MediaID: 0, Decision: "approve"}).Validate())
	assertBadRequest(t, (&moderation_models.ReviewQuarantineInput{MediaID: 1, PostID: -1, Decision: "approve"}).Validate())
	assertBadRequest(t, (&moderation_models.ReviewQuarantineInput{MediaID: 1, Decision: "unknown"}).Validate())
	assertBadRequest(t, (&moderation_models.ReviewQuarantineInput{MediaID: 1, Decision: "reject_minor", Reason: strings.Repeat("x", 501)}).Validate())
	assert.NoError(t, (&moderation_models.ReviewQuarantineInput{MediaID: 10, PostID: 0, Decision: "reject_major", Reason: "Illégal"}).Validate())
	assert.NoError(t, (&moderation_models.ReviewQuarantineInput{MediaID: 10, PostID: 5, Decision: "approve"}).Validate())
	assert.NoError(t, (&moderation_models.ReviewQuarantineInput{MediaID: 10, PostID: 5, Decision: "reject_minor", Reason: "Contenu limite"}).Validate())
}
