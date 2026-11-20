package message_models_test

import (
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateMessageInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}
	var nilIn *message_models.CreateMessageInput
	assertBadRequest(t, nilIn.Validate())

	require.NoError(t, (&message_models.CreateMessageInput{
		ConversationID: 1,
		MessageType:    variables.MessageTypeText,
		Content:        strings.Repeat("é", 2200),
	}).Validate())
	require.NoError(t, (&message_models.CreateMessageInput{
		ConversationID: 1,
		MessageType:    variables.MessageTypeMedia,
		Attachments:    map[string]any{"media_id": int64(55), "ia_score": 0.5},
	}).Validate())
	require.NoError(t, (&message_models.CreateMessageInput{
		ConversationID: 1,
		MessageType:    variables.MessageTypeMedia,
		Attachments:    map[string]any{"media_id": float64(12), "ia_score": nil},
	}).Validate())
	require.NoError(t, (&message_models.CreateMessageInput{
		ConversationID: 1,
		MessageType:    variables.MessageTypeMedia,
		Attachments:    map[string]any{"media_id": int(7)},
	}).Validate())
	require.NoError(t, (&message_models.CreateMessageInput{
		ConversationID: 1,
		MessageType:    variables.MessageTypeWidget,
		Attachments:    map[string]any{"widget": message_models.WidgetPayload{Type: "poll", Title: "Vote"}},
	}).Validate())

	assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 0, MessageType: variables.MessageTypeText, Content: "hi"}).Validate())
	assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: -1, Content: "hi"}).Validate())
	assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: 99, Content: "hi"}).Validate())
	assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeText, Content: "   "}).Validate())
	assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeText, Content: strings.Repeat("é", 2201)}).Validate())
	assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeText, Content: "hi", ThreadParentID: -1}).Validate())
	assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeMedia}).Validate())
	assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeMedia, Attachments: map[string]any{}}).Validate())
	assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeMedia, Attachments: map[string]any{"media_id": 0}}).Validate())
	assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeMedia, Attachments: map[string]any{"media_id": "invalid"}}).Validate())
	assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeMedia, Attachments: map[string]any{"media_id": 5, "ia_score": math.NaN()}}).Validate())
	assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeMedia, Attachments: map[string]any{"media_id": 5, "ia_score": math.Inf(1)}}).Validate())
	assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeMedia, Attachments: map[string]any{"media_id": 5, "ia_score": -0.1}}).Validate())
	assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeMedia, Attachments: map[string]any{"media_id": 5, "ia_score": 1.1}}).Validate())
	assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeWidget}).Validate())
	assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeWidget, Attachments: map[string]any{"widget": map[string]any{"type": "bad"}}}).Validate())
	assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeWidget, Attachments: map[string]any{"widget": make(chan int)}}).Validate())
}
