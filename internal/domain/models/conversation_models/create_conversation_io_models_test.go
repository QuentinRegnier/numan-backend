package conversation_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateConversationInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *conversation_models.CreateConversationInput
	assertBadRequest(t, nilIn.Validate())
	require.NoError(t, (&conversation_models.CreateConversationInput{
		Type:           variables.ConversationTypeDirect,
		ParticipantIDs: []int64{42},
	}).Validate())
	require.NoError(t, (&conversation_models.CreateConversationInput{
		Type:           variables.ConversationTypeGroup,
		Title:          "Mon Groupe",
		ParticipantIDs: []int64{42, 43},
	}).Validate())
	assertBadRequest(t, (&conversation_models.CreateConversationInput{Type: -1, ParticipantIDs: []int64{1}}).Validate())
	assertBadRequest(t, (&conversation_models.CreateConversationInput{Type: 99, ParticipantIDs: []int64{1}}).Validate())
	assertBadRequest(t, (&conversation_models.CreateConversationInput{Type: variables.ConversationTypeGroup, Title: strings.Repeat("é", 101)}).Validate())
	assertBadRequest(t, (&conversation_models.CreateConversationInput{Type: variables.ConversationTypeGroup, Title: "Ok", ParticipantIDs: nil}).Validate())
	assertBadRequest(t, (&conversation_models.CreateConversationInput{Type: variables.ConversationTypeGroup, Title: "Ok", ParticipantIDs: []int64{0}}).Validate())
	assertBadRequest(t, (&conversation_models.CreateConversationInput{
		Type:           variables.ConversationTypeGroup,
		Title:          "Ok",
		ParticipantIDs: []int64{1},
		Settings:       conversation_models.ConversationSettings{WritePermission: 9},
	}).Validate())

	tooMany := make([]int64, variables.MaxAddMemberCount+1)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	assertBadRequest(t, (&conversation_models.CreateConversationInput{Type: variables.ConversationTypeGroup, Title: "Ok", ParticipantIDs: tooMany}).Validate())
}
