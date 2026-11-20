package message_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetMessagesInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}
	var nilIn *message_models.GetMessagesInput
	assertBadRequest(t, nilIn.Validate())

	assertBadRequest(t, (&message_models.GetMessagesInput{ConversationID: 0}).Validate())
	assertBadRequest(t, (&message_models.GetMessagesInput{ConversationID: 1, Limit: -1}).Validate())
	assertBadRequest(t, (&message_models.GetMessagesInput{ConversationID: 1, Limit: 101}).Validate())
	assertBadRequest(t, (&message_models.GetMessagesInput{ConversationID: 1, OffsetID: -1}).Validate())
	assertBadRequest(t, (&message_models.GetMessagesInput{ConversationID: 1, Direction: "side"}).Validate())

	require.NoError(t, (&message_models.GetMessagesInput{ConversationID: 1, Limit: 50, Direction: "bottom"}).Validate())
	require.NoError(t, (&message_models.GetMessagesInput{ConversationID: 1, Limit: 100, OffsetID: 0, Direction: "top"}).Validate())
	require.NoError(t, (&message_models.GetMessagesInput{ConversationID: 1}).Validate())
}
