package member_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateMemberSettingsInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *member_models.UpdateMemberSettingsInput
	assertBadRequest(t, nilIn.Validate())
	assertBadRequest(t, (&member_models.UpdateMemberSettingsInput{ConversationID: 0}).Validate())
	assertBadRequest(t, (&member_models.UpdateMemberSettingsInput{ConversationID: -1}).Validate())
	assertBadRequest(t, (&member_models.UpdateMemberSettingsInput{ConversationID: 1, IsMuted: -1}).Validate())
	assertBadRequest(t, (&member_models.UpdateMemberSettingsInput{ConversationID: 1, IsMuted: 3}).Validate())
	assertBadRequest(t, (&member_models.UpdateMemberSettingsInput{ConversationID: 1, MuteExpiresAt: -2}).Validate())
	require.NoError(t, (&member_models.UpdateMemberSettingsInput{ConversationID: 1, IsMuted: 0, MuteExpiresAt: 0}).Validate())
	require.NoError(t, (&member_models.UpdateMemberSettingsInput{ConversationID: 1, IsMuted: 2, MuteExpiresAt: -1, MediaAutoDownload: true}).Validate())
}
