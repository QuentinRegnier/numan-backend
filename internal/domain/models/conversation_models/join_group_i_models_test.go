package conversation_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJoinGroupInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *conversation_models.JoinGroupInput
	assertBadRequest(t, nilIn.Validate())
	require.NoError(t, (&conversation_models.JoinGroupInput{ConversationID: 1, Internal: true}).Validate())
	require.NoError(t, (&conversation_models.JoinGroupInput{ConversationID: 1, External: true}).Validate())
	require.NoError(t, (&conversation_models.JoinGroupInput{ConversationID: 1, InviteMsgID: 55}).Validate())
	assertBadRequest(t, (&conversation_models.JoinGroupInput{ConversationID: 0, Internal: true}).Validate())
	assertBadRequest(t, (&conversation_models.JoinGroupInput{ConversationID: 1, InviteMsgID: -1}).Validate())
	assertBadRequest(t, (&conversation_models.JoinGroupInput{ConversationID: 1, Internal: true, External: true}).Validate())
}
