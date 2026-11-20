package member_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnbanMembersInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *member_models.UnbanMembersInput
	assertBadRequest(t, nilIn.Validate())
	assertBadRequest(t, (&member_models.UnbanMembersInput{ConversationID: 0, TargetUserIDs: []int64{1}}).Validate())
	assertBadRequest(t, (&member_models.UnbanMembersInput{ConversationID: -1, TargetUserIDs: []int64{1}}).Validate())
	assertBadRequest(t, (&member_models.UnbanMembersInput{ConversationID: 1, TargetUserIDs: nil}).Validate())
	assertBadRequest(t, (&member_models.UnbanMembersInput{ConversationID: 1, TargetUserIDs: []int64{}}).Validate())
	assertBadRequest(t, (&member_models.UnbanMembersInput{ConversationID: 1, TargetUserIDs: []int64{1, 0}}).Validate())
	assertBadRequest(t, (&member_models.UnbanMembersInput{ConversationID: 1, TargetUserIDs: []int64{1, -2}}).Validate())

	tooMany := make([]int64, 101)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	assertBadRequest(t, (&member_models.UnbanMembersInput{ConversationID: 1, TargetUserIDs: tooMany}).Validate())

	require.NoError(t, (&member_models.UnbanMembersInput{ConversationID: 1, TargetUserIDs: []int64{2, 3}}).Validate())
	require.NoError(t, (&member_models.UnbanMembersInput{ConversationID: 1, TargetUserIDs: tooMany[:100]}).Validate())
}
