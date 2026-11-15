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

func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestConversationSettingsAndPayload_Validate(t *testing.T) {
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

func TestConversationMutationInputs_Validate(t *testing.T) {
	t.Run("CreateConversationInput", func(t *testing.T) {
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
		assertBadRequest(t, (&conversation_models.CreateConversationInput{Type: 99, ParticipantIDs: []int64{1}}).Validate())
		assertBadRequest(t, (&conversation_models.CreateConversationInput{Type: variables.ConversationTypeGroup, Title: strings.Repeat("é", 101)}).Validate())
		assertBadRequest(t, (&conversation_models.CreateConversationInput{Type: variables.ConversationTypeGroup, Title: "Ok", ParticipantIDs: []int64{0}}).Validate())
		tooMany := make([]int64, variables.MaxAddMemberCount+1)
		for i := range tooMany {
			tooMany[i] = int64(i + 1)
		}
		assertBadRequest(t, (&conversation_models.CreateConversationInput{Type: variables.ConversationTypeGroup, Title: "Ok", ParticipantIDs: tooMany}).Validate())
	})

	t.Run("CreateCommunityInput", func(t *testing.T) {
		var nilIn *conversation_models.CreateCommunityInput
		assertBadRequest(t, nilIn.Validate())
		require.NoError(t, (&conversation_models.CreateCommunityInput{Title: "Numan Dev"}).Validate())
		assertBadRequest(t, (&conversation_models.CreateCommunityInput{Title: "ab"}).Validate())
		assertBadRequest(t, (&conversation_models.CreateCommunityInput{Title: strings.Repeat("é", 101)}).Validate())
		assertBadRequest(t, (&conversation_models.CreateCommunityInput{Title: "Numan", OwnerID: -1}).Validate())
		assertBadRequest(t, (&conversation_models.CreateCommunityInput{Title: "Numan", ExternalLink: models.ExternalLinks{URL: "bad"}}).Validate())
	})

	t.Run("UpdateConversationInput", func(t *testing.T) {
		var nilIn *conversation_models.UpdateConversationInput
		assertBadRequest(t, nilIn.Validate())
		require.NoError(t, (&conversation_models.UpdateConversationInput{ConversationID: 1, Title: "Nouveau titre"}).Validate())
		assertBadRequest(t, (&conversation_models.UpdateConversationInput{ConversationID: 0}).Validate())
		assertBadRequest(t, (&conversation_models.UpdateConversationInput{ConversationID: 1, Title: strings.Repeat("é", 101)}).Validate())
		assertBadRequest(t, (&conversation_models.UpdateConversationInput{ConversationID: 1, Description: strings.Repeat("é", 501)}).Validate())
		assertBadRequest(t, (&conversation_models.UpdateConversationInput{ConversationID: 1, AvatarID: -5}).Validate())
	})

	t.Run("AddMemberInput", func(t *testing.T) {
		var nilIn *conversation_models.AddMemberInput
		assertBadRequest(t, nilIn.Validate())
		require.NoError(t, (&conversation_models.AddMemberInput{ConversationID: 1, ParticipantIDs: []int64{2, 3}}).Validate())
		assertBadRequest(t, (&conversation_models.AddMemberInput{ConversationID: 0, ParticipantIDs: []int64{2}}).Validate())
		assertBadRequest(t, (&conversation_models.AddMemberInput{ConversationID: 1, ParticipantIDs: nil}).Validate())
		assertBadRequest(t, (&conversation_models.AddMemberInput{ConversationID: 1, ParticipantIDs: []int64{-1}}).Validate())
	})

	t.Run("JoinGroupInput", func(t *testing.T) {
		var nilIn *conversation_models.JoinGroupInput
		assertBadRequest(t, nilIn.Validate())
		require.NoError(t, (&conversation_models.JoinGroupInput{ConversationID: 1, Internal: true}).Validate())
		require.NoError(t, (&conversation_models.JoinGroupInput{ConversationID: 1, External: true}).Validate())
		require.NoError(t, (&conversation_models.JoinGroupInput{ConversationID: 1, InviteMsgID: 55}).Validate())
		assertBadRequest(t, (&conversation_models.JoinGroupInput{ConversationID: 0, Internal: true}).Validate())
		assertBadRequest(t, (&conversation_models.JoinGroupInput{ConversationID: 1, InviteMsgID: -1}).Validate())
		assertBadRequest(t, (&conversation_models.JoinGroupInput{ConversationID: 1, Internal: true, External: true}).Validate())
	})

	t.Run("LeavePinUnpinReadInputs", func(t *testing.T) {
		var nilLeave *conversation_models.LeaveConversationInput
		assertBadRequest(t, nilLeave.Validate())
		assertBadRequest(t, (&conversation_models.LeaveConversationInput{ConversationID: 0}).Validate())
		assertBadRequest(t, (&conversation_models.LeaveConversationInput{ConversationID: 1, NewOwnerID: -1}).Validate())
		require.NoError(t, (&conversation_models.LeaveConversationInput{ConversationID: 1, NewOwnerID: 2}).Validate())

		assertBadRequest(t, (&conversation_models.PinConversationInput{ConversationID: 0}).Validate())
		require.NoError(t, (&conversation_models.PinConversationInput{ConversationID: 1}).Validate())

		assertBadRequest(t, (&conversation_models.UnpinConversationInput{ConversationID: -1}).Validate())
		require.NoError(t, (&conversation_models.UnpinConversationInput{ConversationID: 1}).Validate())

		assertBadRequest(t, (&conversation_models.ReadReceiptInput{ConversationID: 0}).Validate())
		require.NoError(t, (&conversation_models.ReadReceiptInput{ConversationID: 1}).Validate())
	})
}

func TestConversationQueryInputs_Validate(t *testing.T) {
	t.Run("GetConversationsInput", func(t *testing.T) {
		var nilIn *conversation_models.GetConversationsInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&conversation_models.GetConversationsInput{}).Validate())
		assertBadRequest(t, (&conversation_models.GetConversationsInput{ConversationIDs: []int64{1, 0}}).Validate())
		require.NoError(t, (&conversation_models.GetConversationsInput{ConversationIDs: []int64{1, 2}}).Validate())
	})

	t.Run("GetUserConversationsInput", func(t *testing.T) {
		var nilIn *conversation_models.GetUserConversationsInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&conversation_models.GetUserConversationsInput{Limit: -1}).Validate())
		assertBadRequest(t, (&conversation_models.GetUserConversationsInput{Limit: 101}).Validate())
		assertBadRequest(t, (&conversation_models.GetUserConversationsInput{Offset: -1}).Validate())
		require.NoError(t, (&conversation_models.GetUserConversationsInput{Limit: 0, Offset: 0, Force: true}).Validate())
		require.NoError(t, (&conversation_models.GetUserConversationsInput{Limit: 50, Offset: 10}).Validate())
	})

	t.Run("GetWatermarksInput", func(t *testing.T) {
		var nilIn *conversation_models.GetWatermarksInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&conversation_models.GetWatermarksInput{ConversationID: 0}).Validate())
		require.NoError(t, (&conversation_models.GetWatermarksInput{ConversationID: 12}).Validate())
	})

	t.Run("SuggestInput", func(t *testing.T) {
		var nilIn *conversation_models.SuggestInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&conversation_models.SuggestInput{ConversationID: -1}).Validate())
		assertBadRequest(t, (&conversation_models.SuggestInput{Query: strings.Repeat("é", 51)}).Validate())
		assertBadRequest(t, (&conversation_models.SuggestInput{Limit: 51}).Validate())
		assertBadRequest(t, (&conversation_models.SuggestInput{Limit: -1}).Validate())
		assertBadRequest(t, (&conversation_models.SuggestInput{Offset: -1}).Validate())
		assertBadRequest(t, (&conversation_models.SuggestInput{Intent: "invalid"}).Validate())
		require.NoError(t, (&conversation_models.SuggestInput{ConversationID: 1, Query: "alice", Limit: 20, Offset: 0, Intent: "group"}).Validate())
	})
}
