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

func TestReviewBlameInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	assertBadRequest(t, (*moderation_models.ReviewBlameInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.ReviewBlameInput{ReportID: 0, Decision: "validate", Rationale: "Motif valide"}).Validate())
	assertBadRequest(t, (&moderation_models.ReviewBlameInput{ReportID: 1, Decision: "invalid", Rationale: "Motif valide"}).Validate())
	assertBadRequest(t, (&moderation_models.ReviewBlameInput{ReportID: 1, Decision: "oppose", Rationale: "ab"}).Validate())
	assertBadRequest(t, (&moderation_models.ReviewBlameInput{ReportID: 1, Decision: "oppose", Rationale: strings.Repeat("r", 1001)}).Validate())
	assert.NoError(t, (&moderation_models.ReviewBlameInput{ReportID: 1, Decision: "validate", Rationale: "Consensus confirmé"}).Validate())
	assert.NoError(t, (&moderation_models.ReviewBlameInput{ReportID: 1, Decision: "oppose", Rationale: "Désaccord motivé"}).Validate())
}
