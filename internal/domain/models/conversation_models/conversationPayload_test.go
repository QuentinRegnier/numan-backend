package conversation_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConversationSettingsAndPayload_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	t.Run("ConversationSettings", func(t *testing.T) {
		var nilS *conversation_models.ConversationSettings
		assertBadRequest(t, nilS.Validate())
		assertBadRequest(t, (&conversation_models.ConversationSettings{WritePermission: -1}).Validate())
		assertBadRequest(t, (&conversation_models.ConversationSettings{WritePermission: 3}).Validate())
		assertBadRequest(t, (&conversation_models.ConversationSettings{JoinWithLinkDuration: -1}).Validate())
		require.NoError(t, (&conversation_models.ConversationSettings{WritePermission: 2, JoinWithLinkDuration: 0}).Validate())
	})

	validConv := func() conversation_models.ConversationPayload {
		return conversation_models.ConversationPayload{
			ID:           10,
			Type:         variables.ConversationTypeCommunityPub,
			Title:        "Communauté Test",
			Description:  "Description courte",
			AvatarID:     1,
			State:        variables.ConversationStateActive,
			Settings:     conversation_models.DefaultConversationSettings(variables.ConversationTypeCommunityPub),
			ExternalLink: models.ExternalLinks{Title: "Web", URL: "https://numan.app"},
		}
	}

	t.Run("ConversationPayload nil fails", func(t *testing.T) {
		var nilC *conversation_models.ConversationPayload
		assertBadRequest(t, nilC.Validate())
	})

	t.Run("ConversationPayload direct empty title allowed", func(t *testing.T) {
		p := validConv()
		p.Type = variables.ConversationTypeDirect
		p.Title = ""
		p.ExternalLink = models.ExternalLinks{}
		require.NoError(t, p.Validate())
	})

	cases := []struct {
		name   string
		mutate func(p *conversation_models.ConversationPayload)
	}{
		{"invalid type negative", func(p *conversation_models.ConversationPayload) { p.Type = -1 }},
		{"invalid type too high", func(p *conversation_models.ConversationPayload) { p.Type = 4 }},
		{"invalid state negative", func(p *conversation_models.ConversationPayload) { p.State = -3 }},
		{"invalid state too high", func(p *conversation_models.ConversationPayload) { p.State = 2 }},
		{"active group empty title", func(p *conversation_models.ConversationPayload) {
			p.Type = variables.ConversationTypeGroup
			p.Title = "   "
		}},
		{"title > 100 runes", func(p *conversation_models.ConversationPayload) { p.Title = strings.Repeat("é", 101) }},
		{"description > 500 runes", func(p *conversation_models.ConversationPayload) { p.Description = strings.Repeat("é", 501) }},
		{"negative avatar id", func(p *conversation_models.ConversationPayload) { p.AvatarID = -1 }},
		{"negative last message id", func(p *conversation_models.ConversationPayload) { p.LastMessageID = -1 }},
		{"invalid settings", func(p *conversation_models.ConversationPayload) { p.Settings.WritePermission = 9 }},
		{"invalid external link", func(p *conversation_models.ConversationPayload) { p.ExternalLink.URL = "ftp://bad" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validConv()
			tc.mutate(&p)
			assertBadRequest(t, p.Validate())
		})
	}
}

func TestDefaultConversationSettings(t *testing.T) {
	pub := conversation_models.DefaultConversationSettings(variables.ConversationTypeCommunityPub)
	assert.Equal(t, 2, pub.WritePermission)
	assert.False(t, pub.JoinApprovalRequired)
	assert.False(t, pub.SendMediaPermission)
	assert.False(t, pub.AddMemberPermission)
	assert.True(t, pub.HideSystemMessages)
	require.NoError(t, pub.Validate())

	dm := conversation_models.DefaultConversationSettings(variables.ConversationTypeDirect)
	assert.Equal(t, 0, dm.WritePermission)
	assert.False(t, dm.JoinApprovalRequired)
	assert.True(t, dm.SendMediaPermission)
	assert.True(t, dm.AddMemberPermission)
	assert.False(t, dm.HideSystemMessages)
	require.NoError(t, dm.Validate())
}
