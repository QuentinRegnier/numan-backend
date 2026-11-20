package lite_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConversationSettings_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilSettings *lite_models.ConversationSettings
	assertBadRequest(t, nilSettings.Validate())

	require.NoError(t, (&lite_models.ConversationSettings{WritePermission: 1, JoinWithLinkDuration: 60}).Validate())
	assertBadRequest(t, (&lite_models.ConversationSettings{WritePermission: -1}).Validate())
	assertBadRequest(t, (&lite_models.ConversationSettings{WritePermission: 3}).Validate())
	assertBadRequest(t, (&lite_models.ConversationSettings{JoinWithLinkDuration: -1}).Validate())
}

func TestConvLiteRequest_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	valid := lite_models.ConvLiteRequest{
		ID:           10,
		Type:         variables.ConversationTypeGroup,
		Title:        "Général",
		Settings:     lite_models.ConversationSettings{WritePermission: 1, JoinWithLinkDuration: 60},
		ExternalLink: models.ExternalLinks{Title: "Site", URL: "https://example.com"},
	}
	require.NoError(t, valid.Validate())

	var nilConv *lite_models.ConvLiteRequest
	assertBadRequest(t, nilConv.Validate())

	bad := valid
	bad.ID = 0
	assertBadRequest(t, bad.Validate())

	bad = valid
	bad.Type = 99
	assertBadRequest(t, bad.Validate())

	bad = valid
	bad.Title = strings.Repeat("a", 101)
	assertBadRequest(t, bad.Validate())

	bad = valid
	bad.Description = strings.Repeat("a", 501)
	assertBadRequest(t, bad.Validate())

	bad = valid
	bad.AvatarID = -1
	assertBadRequest(t, bad.Validate())

	bad = valid
	bad.LastMessageID = -1
	assertBadRequest(t, bad.Validate())

	bad = valid
	bad.Settings.WritePermission = 3
	assertBadRequest(t, bad.Validate())

	bad = valid
	bad.ExternalLink.URL = "ftp://invalid"
	assertBadRequest(t, bad.Validate())
}
