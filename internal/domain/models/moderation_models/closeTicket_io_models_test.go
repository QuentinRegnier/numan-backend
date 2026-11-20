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

func TestCloseTicketInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	assertBadRequest(t, (*moderation_models.CloseTicketInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.CloseTicketInput{ReportID: 0, Rationale: "valid rationale"}).Validate())
	assertBadRequest(t, (&moderation_models.CloseTicketInput{ReportID: 1, Rationale: "ab"}).Validate())
	assertBadRequest(t, (&moderation_models.CloseTicketInput{ReportID: 1, Rationale: "   "}).Validate())
	assertBadRequest(t, (&moderation_models.CloseTicketInput{ReportID: 1, Rationale: strings.Repeat("x", 1001)}).Validate())
	assert.NoError(t, (&moderation_models.CloseTicketInput{ReportID: 1, IsDismiss: true, Rationale: "Signalement clos"}).Validate())
	assert.NoError(t, (&moderation_models.CloseTicketInput{ReportID: 1, IsDismiss: false, Rationale: strings.Repeat("x", 1000)}).Validate())
}
