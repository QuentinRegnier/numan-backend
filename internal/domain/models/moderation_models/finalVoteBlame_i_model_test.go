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

func TestFinalVoteBlameInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	assertBadRequest(t, (*moderation_models.FinalVoteBlameInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.FinalVoteBlameInput{ReportID: 0, Decision: "confirm_blame", Rationale: "Motif valide"}).Validate())
	assertBadRequest(t, (&moderation_models.FinalVoteBlameInput{ReportID: 1, Decision: "other", Rationale: "Motif valide"}).Validate())
	assertBadRequest(t, (&moderation_models.FinalVoteBlameInput{ReportID: 1, Decision: "reject_blame", Rationale: "ok"}).Validate())
	assertBadRequest(t, (&moderation_models.FinalVoteBlameInput{ReportID: 1, Decision: "reject_blame", Rationale: strings.Repeat("x", 1001)}).Validate())
	assert.NoError(t, (&moderation_models.FinalVoteBlameInput{ReportID: 1, Decision: "confirm_blame", Rationale: "Arbitrage confirmé"}).Validate())
	assert.NoError(t, (&moderation_models.FinalVoteBlameInput{ReportID: 1, Decision: "reject_blame", Rationale: "Blâme rejeté"}).Validate())
}
