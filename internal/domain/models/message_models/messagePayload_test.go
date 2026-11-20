package message_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessagePayload_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}
	var nilM *message_models.MessagePayload
	assertBadRequest(t, nilM.Validate())

	valid := message_models.MessagePayload{
		ID:             1,
		ConversationID: 10,
		SenderID:       20,
		MessageType:    variables.MessageTypeText,
		Visibility:     true,
		Content:        "Bonjour à tous",
	}
	require.NoError(t, valid.Validate())

	require.NoError(t, (&message_models.MessagePayload{
		ConversationID: 1,
		SenderID:       1,
		MessageType:    variables.MessageTypeText,
		Visibility:     false,
		Content:        "",
	}).Validate())

	require.NoError(t, (&message_models.MessagePayload{
		ConversationID: 1,
		SenderID:       1,
		MessageType:    variables.MessageTypeWidget,
		Visibility:     true,
		Attachments:    map[string]any{"widget": message_models.WidgetPayload{Type: "poll", Title: "Vote"}},
	}).Validate())

	require.NoError(t, (&message_models.MessagePayload{
		ConversationID: 1,
		SenderID:       1,
		MessageType:    variables.MessageTypeWidget,
		Visibility:     false,
	}).Validate())

	assertBadRequest(t, (&message_models.MessagePayload{ConversationID: 0, SenderID: 1, MessageType: variables.MessageTypeText, Content: "x", Visibility: true}).Validate())
	assertBadRequest(t, (&message_models.MessagePayload{ConversationID: 1, SenderID: 0, MessageType: variables.MessageTypeText, Content: "x", Visibility: true}).Validate())
	assertBadRequest(t, (&message_models.MessagePayload{ConversationID: 1, SenderID: 1, MessageType: -1}).Validate())
	assertBadRequest(t, (&message_models.MessagePayload{ConversationID: 1, SenderID: 1, MessageType: 99}).Validate())
	assertBadRequest(t, (&message_models.MessagePayload{ConversationID: 1, SenderID: 1, MessageType: variables.MessageTypeText, Content: "   ", Visibility: true}).Validate())
	assertBadRequest(t, (&message_models.MessagePayload{ConversationID: 1, SenderID: 1, MessageType: variables.MessageTypeText, Content: strings.Repeat("é", 2201), Visibility: true}).Validate())
	assertBadRequest(t, (&message_models.MessagePayload{ConversationID: 1, SenderID: 1, MessageType: variables.MessageTypeWidget, Visibility: true}).Validate())
	assertBadRequest(t, (&message_models.MessagePayload{ConversationID: 1, SenderID: 1, MessageType: variables.MessageTypeWidget, Visibility: true, Attachments: map[string]any{"widget": make(chan int)}}).Validate())
	assertBadRequest(t, (&message_models.MessagePayload{ConversationID: 1, SenderID: 1, MessageType: variables.MessageTypeWidget, Visibility: true, Attachments: map[string]any{"widget": map[string]any{"type": "bad"}}}).Validate())
}
