package moderation_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReleaseTicketInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	assertBadRequest(t, (*moderation_models.ReleaseTicketInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.ReleaseTicketInput{ReportID: -2}).Validate())
	assertBadRequest(t, (&moderation_models.ReleaseTicketInput{ReportID: 0}).Validate())
	assert.NoError(t, (&moderation_models.ReleaseTicketInput{ReportID: 1}).Validate())
}
