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

func TestReplyTicketInfoInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	assertBadRequest(t, (*moderation_models.ReplyTicketInfoInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.ReplyTicketInfoInput{ReportID: 0, Reply: "Voici le contexte"}).Validate())
	assertBadRequest(t, (&moderation_models.ReplyTicketInfoInput{ReportID: 1, Reply: "   "}).Validate())
	assertBadRequest(t, (&moderation_models.ReplyTicketInfoInput{ReportID: 1, Reply: strings.Repeat("a", 2001)}).Validate())
	assert.NoError(t, (&moderation_models.ReplyTicketInfoInput{ReportID: 1, Reply: "Voici le contexte"}).Validate())
}
