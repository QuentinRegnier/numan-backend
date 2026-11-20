package member_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMuteMemberInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *member_models.MuteMemberInput
	assertBadRequest(t, nilIn.Validate())
	assertBadRequest(t, (&member_models.MuteMemberInput{ConversationID: 0, TargetUserID: 2, RestrictedUntil: 100}).Validate())
	assertBadRequest(t, (&member_models.MuteMemberInput{ConversationID: -1, TargetUserID: 2, RestrictedUntil: 100}).Validate())
	assertBadRequest(t, (&member_models.MuteMemberInput{ConversationID: 1, TargetUserID: 0, RestrictedUntil: 100}).Validate())
	assertBadRequest(t, (&member_models.MuteMemberInput{ConversationID: 1, TargetUserID: -2, RestrictedUntil: 100}).Validate())
	assertBadRequest(t, (&member_models.MuteMemberInput{ConversationID: 1, TargetUserID: 2, RestrictedUntil: -1}).Validate())
	require.NoError(t, (&member_models.MuteMemberInput{ConversationID: 1, TargetUserID: 2, RestrictedUntil: 0}).Validate())
	require.NoError(t, (&member_models.MuteMemberInput{ConversationID: 1, TargetUserID: 2, RestrictedUntil: 100}).Validate())
}
