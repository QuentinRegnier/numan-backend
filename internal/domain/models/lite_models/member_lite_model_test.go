package lite_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemberSettingsLite_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilSettings *lite_models.MemberSettingsLite
	assertBadRequest(t, nilSettings.Validate())

	require.NoError(t, (&lite_models.MemberSettingsLite{Pinned: -1, MuteExpireAt: -1}).Validate())
	assertBadRequest(t, (&lite_models.MemberSettingsLite{IsMuted: -1}).Validate())
	assertBadRequest(t, (&lite_models.MemberSettingsLite{IsMuted: 3}).Validate())
	assertBadRequest(t, (&lite_models.MemberSettingsLite{MuteExpireAt: -2}).Validate())
	assertBadRequest(t, (&lite_models.MemberSettingsLite{Pinned: -2}).Validate())
	assertBadRequest(t, (&lite_models.MemberSettingsLite{Pinned: 99}).Validate())
	assertBadRequest(t, (&lite_models.MemberSettingsLite{RestrictedUntil: -1}).Validate())
}

func TestMemberLiteRequest_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	valid := lite_models.MemberLiteRequest{
		ConversationID: 1,
		UserID:         2,
		Role:           variables.MemberRoleNormal,
		Settings:       lite_models.MemberSettingsLite{Pinned: -1},
	}
	require.NoError(t, valid.Validate())

	var nilMem *lite_models.MemberLiteRequest
	assertBadRequest(t, nilMem.Validate())
	assertBadRequest(t, (&lite_models.MemberLiteRequest{ConversationID: 0, UserID: 2}).Validate())
	assertBadRequest(t, (&lite_models.MemberLiteRequest{ConversationID: 1, UserID: 0}).Validate())
	assertBadRequest(t, (&lite_models.MemberLiteRequest{ConversationID: 1, UserID: 2, Role: 99}).Validate())
	assertBadRequest(t, (&lite_models.MemberLiteRequest{ConversationID: 1, UserID: 2, UnreadCount: -1}).Validate())
	assertBadRequest(t, (&lite_models.MemberLiteRequest{ConversationID: 1, UserID: 2, FrozenMessageID: -1}).Validate())
	assertBadRequest(t, (&lite_models.MemberLiteRequest{ConversationID: 1, UserID: 2, LastReadMessageID: -1}).Validate())
	assertBadRequest(t, (&lite_models.MemberLiteRequest{ConversationID: 1, UserID: 2, JoinedAt: -1}).Validate())
	assertBadRequest(t, (&lite_models.MemberLiteRequest{ConversationID: 1, UserID: 2, Settings: lite_models.MemberSettingsLite{Pinned: 99}}).Validate())
}
