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

func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestWidgetPayload_Validate(t *testing.T) {
	valid := func() message_models.WidgetPayload {
		return message_models.WidgetPayload{
			Type:        "action_button",
			Title:       "Rejoindre l'événement",
			Description: "Ce soir à 20h",
			Action:      message_models.WidgetAction{Label: "Voir", URL: "https://example.com"},
			Visuals:     message_models.WidgetVisuals{Style: "primary", Icon: "star", CoverMediaID: 10},
		}
	}

	var nilW *message_models.WidgetPayload
	assertBadRequest(t, nilW.Validate())
	w := valid()
	require.NoError(t, w.Validate())

	for _, tc := range []struct {
		name   string
		mutate func(*message_models.WidgetPayload)
	}{
		{"unsupported type", func(p *message_models.WidgetPayload) { p.Type = "unknown" }},
		{"title > 120 runes", func(p *message_models.WidgetPayload) { p.Title = strings.Repeat("é", 121) }},
		{"description > 500 runes", func(p *message_models.WidgetPayload) { p.Description = strings.Repeat("é", 501) }},
		{"action label > 80 runes", func(p *message_models.WidgetPayload) { p.Action.Label = strings.Repeat("a", 81) }},
		{"action url > 2048 chars", func(p *message_models.WidgetPayload) { p.Action.URL = strings.Repeat("a", 2049) }},
		{"negative cover media id", func(p *message_models.WidgetPayload) { p.Visuals.CoverMediaID = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cp := valid()
			tc.mutate(&cp)
			assertBadRequest(t, cp.Validate())
		})
	}
}

func TestMessageAndReactionPayloads_Validate(t *testing.T) {
	t.Run("MessagePayload", func(t *testing.T) {
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

		assertBadRequest(t, (&message_models.MessagePayload{ConversationID: 0, SenderID: 1, MessageType: variables.MessageTypeText, Content: "x", Visibility: true}).Validate())
		assertBadRequest(t, (&message_models.MessagePayload{ConversationID: 1, SenderID: 0, MessageType: variables.MessageTypeText, Content: "x", Visibility: true}).Validate())
		assertBadRequest(t, (&message_models.MessagePayload{ConversationID: 1, SenderID: 1, MessageType: 99}).Validate())
		assertBadRequest(t, (&message_models.MessagePayload{ConversationID: 1, SenderID: 1, MessageType: variables.MessageTypeText, Content: "   ", Visibility: true}).Validate())
		assertBadRequest(t, (&message_models.MessagePayload{ConversationID: 1, SenderID: 1, MessageType: variables.MessageTypeText, Content: strings.Repeat("é", 2201), Visibility: true}).Validate())
	})

	t.Run("MessageReactionPayload", func(t *testing.T) {
		var nilR *message_models.MessageReactionPayload
		assertBadRequest(t, nilR.Validate())
		require.NoError(t, (&message_models.MessageReactionPayload{MessageID: 1, UserID: 2, Reaction: "🔥"}).Validate())
		assertBadRequest(t, (&message_models.MessageReactionPayload{MessageID: 0, UserID: 2, Reaction: "🔥"}).Validate())
		assertBadRequest(t, (&message_models.MessageReactionPayload{MessageID: 1, UserID: 0, Reaction: "🔥"}).Validate())
		assertBadRequest(t, (&message_models.MessageReactionPayload{MessageID: 1, UserID: 2, Reaction: "  "}).Validate())
		assertBadRequest(t, (&message_models.MessageReactionPayload{MessageID: 1, UserID: 2, Reaction: strings.Repeat("a", 33)}).Validate())
	})
}

func TestMessageInputs_Validate(t *testing.T) {
	t.Run("CreateMessageInput", func(t *testing.T) {
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
			MessageType:    variables.MessageTypeWidget,
			Attachments:    map[string]any{"widget": message_models.WidgetPayload{Type: "poll", Title: "Vote"}},
		}).Validate())

		assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 0, MessageType: variables.MessageTypeText, Content: "hi"}).Validate())
		assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: 99, Content: "hi"}).Validate())
		assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeText, Content: "   "}).Validate())
		assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeText, Content: strings.Repeat("é", 2201)}).Validate())
		assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeText, Content: "hi", ThreadParentID: -1}).Validate())
		assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeMedia}).Validate())
		assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeMedia, Attachments: map[string]any{"media_id": 0}}).Validate())
		assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeMedia, Attachments: map[string]any{"media_id": 5, "ia_score": math.NaN()}}).Validate())
		assertBadRequest(t, (&message_models.CreateMessageInput{ConversationID: 1, MessageType: variables.MessageTypeWidget, Attachments: map[string]any{"widget": map[string]any{"type": "bad"}}}).Validate())
	})

	t.Run("Update and Delete inputs", func(t *testing.T) {
		assertBadRequest(t, (&message_models.UpdateMessageInput{MessageID: 0, Content: "hi"}).Validate())
		assertBadRequest(t, (&message_models.UpdateMessageInput{MessageID: 1, Content: "   "}).Validate())
		assertBadRequest(t, (&message_models.UpdateMessageInput{MessageID: 1, Content: strings.Repeat("é", 2201)}).Validate())
		require.NoError(t, (&message_models.UpdateMessageInput{MessageID: 1, Content: "Mis à jour"}).Validate())

		assertBadRequest(t, (&message_models.DeleteMessageInput{MessageID: 0}).Validate())
		require.NoError(t, (&message_models.DeleteMessageInput{MessageID: 1}).Validate())
	})

	t.Run("GetMessages and Reactions inputs", func(t *testing.T) {
		assertBadRequest(t, (&message_models.GetMessagesInput{ConversationID: 0}).Validate())
		assertBadRequest(t, (&message_models.GetMessagesInput{ConversationID: 1, Limit: 101}).Validate())
		assertBadRequest(t, (&message_models.GetMessagesInput{ConversationID: 1, OffsetID: -1}).Validate())
		assertBadRequest(t, (&message_models.GetMessagesInput{ConversationID: 1, Direction: "side"}).Validate())
		require.NoError(t, (&message_models.GetMessagesInput{ConversationID: 1, Limit: 50, Direction: "bottom"}).Validate())

		assertBadRequest(t, (&message_models.GetMessageReactionsInput{MessageID: 0}).Validate())
		assertBadRequest(t, (&message_models.GetMessageReactionsInput{MessageID: 1, Limit: 101}).Validate())
		assertBadRequest(t, (&message_models.GetMessageReactionsInput{MessageID: 1, Offset: -1}).Validate())
		neg := int64(-1)
		assertBadRequest(t, (&message_models.GetMessageReactionsInput{MessageID: 1, BeforeCreatedAt: &neg}).Validate())
		assertBadRequest(t, (&message_models.GetMessageReactionsInput{MessageID: 1, BeforeID: &neg}).Validate())
		require.NoError(t, (&message_models.GetMessageReactionsInput{MessageID: 1, Limit: 20}).Validate())

		assertBadRequest(t, (&message_models.ReactMessageInput{MessageID: 0, Reaction: "👍"}).Validate())
		assertBadRequest(t, (&message_models.ReactMessageInput{MessageID: 1, Reaction: "  "}).Validate())
		assertBadRequest(t, (&message_models.ReactMessageInput{MessageID: 1, Reaction: strings.Repeat("a", 33)}).Validate())
		require.NoError(t, (&message_models.ReactMessageInput{MessageID: 1, Reaction: "❤️"}).Validate())

		assertBadRequest(t, (&message_models.UnreactMessageInput{MessageID: 0}).Validate())
		require.NoError(t, (&message_models.UnreactMessageInput{MessageID: 1}).Validate())
	})
}
