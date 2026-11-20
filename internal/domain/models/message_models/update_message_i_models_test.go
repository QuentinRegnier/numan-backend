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

func TestUpdateMessageInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}
	var nilIn *message_models.UpdateMessageInput
	assertBadRequest(t, nilIn.Validate())

	assertBadRequest(t, (&message_models.UpdateMessageInput{MessageID: 0, Content: "hi"}).Validate())
	assertBadRequest(t, (&message_models.UpdateMessageInput{MessageID: 1, Content: "   "}).Validate())
	assertBadRequest(t, (&message_models.UpdateMessageInput{MessageID: 1, Content: strings.Repeat("é", 2201)}).Validate())

	require.NoError(t, (&message_models.UpdateMessageInput{MessageID: 1, Content: "Mis à jour"}).Validate())
	require.NoError(t, (&message_models.UpdateMessageInput{MessageID: 1, Content: strings.Repeat("é", 2200)}).Validate())
}
