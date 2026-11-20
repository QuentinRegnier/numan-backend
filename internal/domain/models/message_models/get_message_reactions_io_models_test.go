package message_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetMessageReactionsInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}
	var nilIn *message_models.GetMessageReactionsInput
	assertBadRequest(t, nilIn.Validate())

	assertBadRequest(t, (&message_models.GetMessageReactionsInput{MessageID: 0}).Validate())
	assertBadRequest(t, (&message_models.GetMessageReactionsInput{MessageID: 1, Limit: -1}).Validate())
	assertBadRequest(t, (&message_models.GetMessageReactionsInput{MessageID: 1, Limit: 101}).Validate())
	assertBadRequest(t, (&message_models.GetMessageReactionsInput{MessageID: 1, Offset: -1}).Validate())

	neg := int64(-1)
	zero := int64(0)
	pos := int64(10)
	assertBadRequest(t, (&message_models.GetMessageReactionsInput{MessageID: 1, BeforeCreatedAt: &neg}).Validate())
	assertBadRequest(t, (&message_models.GetMessageReactionsInput{MessageID: 1, BeforeID: &neg}).Validate())
	assertBadRequest(t, (&message_models.GetMessageReactionsInput{MessageID: 1, BeforeID: &zero}).Validate())

	require.NoError(t, (&message_models.GetMessageReactionsInput{MessageID: 1, Limit: 20}).Validate())
	require.NoError(t, (&message_models.GetMessageReactionsInput{
		MessageID:       1,
		Limit:           100,
		Offset:          0,
		BeforeCreatedAt: &zero,
		BeforeID:        &pos,
	}).Validate())
}
