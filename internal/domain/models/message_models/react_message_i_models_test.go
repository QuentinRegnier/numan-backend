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

func TestReactMessageInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}
	var nilIn *message_models.ReactMessageInput
	assertBadRequest(t, nilIn.Validate())

	assertBadRequest(t, (&message_models.ReactMessageInput{MessageID: 0, Reaction: "👍"}).Validate())
	assertBadRequest(t, (&message_models.ReactMessageInput{MessageID: 1, Reaction: "  "}).Validate())
	assertBadRequest(t, (&message_models.ReactMessageInput{MessageID: 1, Reaction: strings.Repeat("a", 33)}).Validate())
	assertBadRequest(t, (&message_models.ReactMessageInput{MessageID: 1, Reaction: strings.Repeat("👍", 31)}).Validate())
	assertBadRequest(t, (&message_models.ReactMessageInput{MessageID: 1, Reaction: "bad\x00"}).Validate())

	require.NoError(t, (&message_models.ReactMessageInput{MessageID: 1, Reaction: "❤️"}).Validate())
	require.NoError(t, (&message_models.ReactMessageInput{MessageID: 1, Reaction: strings.Repeat("👍", 30)}).Validate())
}
