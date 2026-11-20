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

func TestEscalateTicketInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	assertBadRequest(t, (*moderation_models.EscalateTicketInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.EscalateTicketInput{ReportID: 0, EscalationType: "hard_ban_request", Reason: "Raison valide"}).Validate())
	assertBadRequest(t, (&moderation_models.EscalateTicketInput{ReportID: 1, EscalationType: "unknown", Reason: "Raison valide"}).Validate())
	assertBadRequest(t, (&moderation_models.EscalateTicketInput{ReportID: 1, EscalationType: "cgu_gray_area", Reason: "abc"}).Validate())
	assertBadRequest(t, (&moderation_models.EscalateTicketInput{ReportID: 1, EscalationType: "cgu_gray_area", Reason: "     "}).Validate())
	assertBadRequest(t, (&moderation_models.EscalateTicketInput{ReportID: 1, EscalationType: "cgu_gray_area", Reason: strings.Repeat("r", 1001)}).Validate())
	assertBadRequest(t, (&moderation_models.EscalateTicketInput{ReportID: 1, EscalationType: "cgu_gray_area", Reason: "Raison valide", CGUFeedbackNote: strings.Repeat("n", 2001)}).Validate())
	assert.NoError(t, (&moderation_models.EscalateTicketInput{ReportID: 1, EscalationType: "hard_ban_request", Reason: "Contenu gravement illicite"}).Validate())
	assert.NoError(t, (&moderation_models.EscalateTicketInput{ReportID: 2, EscalationType: "cgu_gray_area", Reason: "Zone grise CGU", CGUFeedbackNote: strings.Repeat("n", 2000)}).Validate())
}
