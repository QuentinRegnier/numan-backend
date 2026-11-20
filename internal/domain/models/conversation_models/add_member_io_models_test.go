package conversation_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddMemberInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *conversation_models.AddMemberInput
	assertBadRequest(t, nilIn.Validate())
	require.NoError(t, (&conversation_models.AddMemberInput{ConversationID: 1, ParticipantIDs: []int64{2, 3}}).Validate())
	assertBadRequest(t, (&conversation_models.AddMemberInput{ConversationID: 0, ParticipantIDs: []int64{2}}).Validate())
	assertBadRequest(t, (&conversation_models.AddMemberInput{ConversationID: 1, ParticipantIDs: nil}).Validate())
	assertBadRequest(t, (&conversation_models.AddMemberInput{ConversationID: 1, ParticipantIDs: []int64{-1}}).Validate())
	assertBadRequest(t, (&conversation_models.AddMemberInput{ConversationID: 1, ParticipantIDs: []int64{0}}).Validate())

	tooMany := make([]int64, variables.MaxAddMemberCount+1)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	assertBadRequest(t, (&conversation_models.AddMemberInput{ConversationID: 1, ParticipantIDs: tooMany}).Validate())
}
