package message_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageReactionPayload_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}
	var nilR *message_models.MessageReactionPayload
	assertBadRequest(t, nilR.Validate())

	require.NoError(t, (&message_models.MessageReactionPayload{MessageID: 1, UserID: 2, Reaction: "🔥"}).Validate())
	require.NoError(t, (&message_models.MessageReactionPayload{MessageID: 1, UserID: 2, Reaction: strings.Repeat("🔥", 30)}).Validate())

	assertBadRequest(t, (&message_models.MessageReactionPayload{MessageID: 0, UserID: 2, Reaction: "🔥"}).Validate())
	assertBadRequest(t, (&message_models.MessageReactionPayload{MessageID: 1, UserID: 0, Reaction: "🔥"}).Validate())
	assertBadRequest(t, (&message_models.MessageReactionPayload{MessageID: 1, UserID: 2, Reaction: "  "}).Validate())
	assertBadRequest(t, (&message_models.MessageReactionPayload{MessageID: 1, UserID: 2, Reaction: strings.Repeat("a", 33)}).Validate())
	assertBadRequest(t, (&message_models.MessageReactionPayload{MessageID: 1, UserID: 2, Reaction: strings.Repeat("🔥", 31)}).Validate())
}
