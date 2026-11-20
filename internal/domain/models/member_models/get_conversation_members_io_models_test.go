package member_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetConversationMembersInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *member_models.GetConversationMembersInput
	assertBadRequest(t, nilIn.Validate())
	assertBadRequest(t, (&member_models.GetConversationMembersInput{ConversationID: 0}).Validate())
	assertBadRequest(t, (&member_models.GetConversationMembersInput{ConversationID: -1}).Validate())
	assertBadRequest(t, (&member_models.GetConversationMembersInput{ConversationID: 1, Limit: -1}).Validate())
	assertBadRequest(t, (&member_models.GetConversationMembersInput{ConversationID: 1, Limit: 101}).Validate())
	assertBadRequest(t, (&member_models.GetConversationMembersInput{ConversationID: 1, Offset: -1}).Validate())
	require.NoError(t, (&member_models.GetConversationMembersInput{ConversationID: 1, Limit: 0, Offset: 0}).Validate())
	require.NoError(t, (&member_models.GetConversationMembersInput{ConversationID: 1, Limit: 100, Offset: 5}).Validate())
}
