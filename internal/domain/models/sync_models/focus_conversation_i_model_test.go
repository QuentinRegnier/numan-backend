package sync_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/sync_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFocusConversationInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *sync_models.FocusConversationInput
	assertBadRequest(t, nilIn.Validate())
	assertBadRequest(t, (&sync_models.FocusConversationInput{ConversationID: 0}).Validate())
	assertBadRequest(t, (&sync_models.FocusConversationInput{ConversationID: -1}).Validate())
	require.NoError(t, (&sync_models.FocusConversationInput{ConversationID: 10, Active: true}).Validate())
	require.NoError(t, (&sync_models.FocusConversationInput{ConversationID: 10, Active: false}).Validate())
}
