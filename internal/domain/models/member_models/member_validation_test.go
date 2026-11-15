package member_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
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

func TestMemberSettingsAndPayload_Validate(t *testing.T) {
	t.Run("MemberSettings", func(t *testing.T) {
		var nilS *member_models.MemberSettings
		assertBadRequest(t, nilS.Validate())
		assertBadRequest(t, (&member_models.MemberSettings{IsMuted: -1}).Validate())
		assertBadRequest(t, (&member_models.MemberSettings{IsMuted: 3}).Validate())
		assertBadRequest(t, (&member_models.MemberSettings{MuteExpireAt: -2}).Validate())
		assertBadRequest(t, (&member_models.MemberSettings{Pinned: -2}).Validate())
		assertBadRequest(t, (&member_models.MemberSettings{Pinned: variables.MaxPinConversation}).Validate())
		assertBadRequest(t, (&member_models.MemberSettings{RestrictedUntil: -1}).Validate())
		require.NoError(t, (&member_models.MemberSettings{IsMuted: 2, MuteExpireAt: -1, Pinned: -1}).Validate())
	})

	validMember := func() member_models.MemberPayload {
		return member_models.MemberPayload{
			ID:             1,
			ConversationID: 10,
			UserID:         20,
			Role:           variables.MemberRoleNormal,
			Settings:       member_models.DefaultMemberSettings(variables.ConversationTypeGroup),
		}
	}

	t.Run("MemberPayload", func(t *testing.T) {
		var nilM *member_models.MemberPayload
		assertBadRequest(t, nilM.Validate())
		m := validMember()
		require.NoError(t, m.Validate())

		for _, tc := range []struct {
			name   string
			mutate func(*member_models.MemberPayload)
		}{
			{"conv id 0", func(p *member_models.MemberPayload) { p.ConversationID = 0 }},
			{"user id 0", func(p *member_models.MemberPayload) { p.UserID = 0 }},
			{"role < min", func(p *member_models.MemberPayload) { p.Role = variables.MemberRolePendingApproval - 1 }},
			{"role > max", func(p *member_models.MemberPayload) { p.Role = variables.MemberRoleOwner + 1 }},
			{"negative unread", func(p *member_models.MemberPayload) { p.UnreadCount = -1 }},
			{"negative frozen", func(p *member_models.MemberPayload) { p.FrozenMessageID = -1 }},
			{"negative last read", func(p *member_models.MemberPayload) { p.LastReadMessageID = -1 }},
			{"invalid settings", func(p *member_models.MemberPayload) { p.Settings.Pinned = 99 }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				cp := validMember()
				tc.mutate(&cp)
				assertBadRequest(t, cp.Validate())
			})
		}
	})
}

func TestMemberInputs_Validate(t *testing.T) {
	t.Run("Accept/Refuse/Ban/Promote/Demote inputs", func(t *testing.T) {
		assertBadRequest(t, (&member_models.AcceptCommunityRequestInput{ConversationID: 0, TargetUserID: 1}).Validate())
		assertBadRequest(t, (&member_models.AcceptCommunityRequestInput{ConversationID: 1, TargetUserID: 0}).Validate())
		require.NoError(t, (&member_models.AcceptCommunityRequestInput{ConversationID: 1, TargetUserID: 2}).Validate())

		assertBadRequest(t, (&member_models.RefuseCommunityRequestInput{ConversationID: 0, TargetUserID: 1}).Validate())
		assertBadRequest(t, (&member_models.RefuseCommunityRequestInput{ConversationID: 1, TargetUserID: 0}).Validate())
		require.NoError(t, (&member_models.RefuseCommunityRequestInput{ConversationID: 1, TargetUserID: 2}).Validate())

		assertBadRequest(t, (&member_models.BanMemberInput{ConversationID: 0, TargetUserID: 1}).Validate())
		assertBadRequest(t, (&member_models.BanMemberInput{ConversationID: 1, TargetUserID: 0}).Validate())
		require.NoError(t, (&member_models.BanMemberInput{ConversationID: 1, TargetUserID: 2}).Validate())

		assertBadRequest(t, (&member_models.PromoteMemberInput{ConversationID: 0, TargetUserID: 1}).Validate())
		assertBadRequest(t, (&member_models.PromoteMemberInput{ConversationID: 1, TargetUserID: 0}).Validate())
		require.NoError(t, (&member_models.PromoteMemberInput{ConversationID: 1, TargetUserID: 2}).Validate())

		assertBadRequest(t, (&member_models.DemoteMemberInput{ConversationID: 0, TargetUserID: 1}).Validate())
		assertBadRequest(t, (&member_models.DemoteMemberInput{ConversationID: 1, TargetUserID: 0}).Validate())
		require.NoError(t, (&member_models.DemoteMemberInput{ConversationID: 1, TargetUserID: 2}).Validate())
	})

	t.Run("MuteMemberInput", func(t *testing.T) {
		var nilIn *member_models.MuteMemberInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&member_models.MuteMemberInput{ConversationID: 0, TargetUserID: 2, RestrictedUntil: 100}).Validate())
		assertBadRequest(t, (&member_models.MuteMemberInput{ConversationID: 1, TargetUserID: 0, RestrictedUntil: 100}).Validate())
		assertBadRequest(t, (&member_models.MuteMemberInput{ConversationID: 1, TargetUserID: 2, RestrictedUntil: -1}).Validate())
		require.NoError(t, (&member_models.MuteMemberInput{ConversationID: 1, TargetUserID: 2, RestrictedUntil: 0}).Validate())
	})

	t.Run("UnbanMembersInput", func(t *testing.T) {
		var nilIn *member_models.UnbanMembersInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&member_models.UnbanMembersInput{ConversationID: 0, TargetUserIDs: []int64{1}}).Validate())
		assertBadRequest(t, (&member_models.UnbanMembersInput{ConversationID: 1, TargetUserIDs: nil}).Validate())
		assertBadRequest(t, (&member_models.UnbanMembersInput{ConversationID: 1, TargetUserIDs: []int64{1, 0}}).Validate())
		require.NoError(t, (&member_models.UnbanMembersInput{ConversationID: 1, TargetUserIDs: []int64{2, 3}}).Validate())
	})

	t.Run("UpdateMemberSettingsInput", func(t *testing.T) {
		var nilIn *member_models.UpdateMemberSettingsInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&member_models.UpdateMemberSettingsInput{ConversationID: 0}).Validate())
		assertBadRequest(t, (&member_models.UpdateMemberSettingsInput{ConversationID: 1, IsMuted: 3}).Validate())
		assertBadRequest(t, (&member_models.UpdateMemberSettingsInput{ConversationID: 1, MuteExpiresAt: -2}).Validate())
		require.NoError(t, (&member_models.UpdateMemberSettingsInput{ConversationID: 1, IsMuted: 2, MuteExpiresAt: -1, MediaAutoDownload: true}).Validate())
	})

	t.Run("Pagination query inputs", func(t *testing.T) {
		assertBadRequest(t, (&member_models.GetBannedMembersInput{ConversationID: 0}).Validate())
		assertBadRequest(t, (&member_models.GetBannedMembersInput{ConversationID: 1, Limit: 101}).Validate())
		assertBadRequest(t, (&member_models.GetBannedMembersInput{ConversationID: 1, Offset: -1}).Validate())
		require.NoError(t, (&member_models.GetBannedMembersInput{ConversationID: 1, Limit: 20, Offset: 0}).Validate())

		assertBadRequest(t, (&member_models.GetMutedMembersInput{ConversationID: 0}).Validate())
		assertBadRequest(t, (&member_models.GetMutedMembersInput{ConversationID: 1, Limit: -1}).Validate())
		require.NoError(t, (&member_models.GetMutedMembersInput{ConversationID: 1, Limit: 0, Offset: 5}).Validate())

		assertBadRequest(t, (&member_models.GetCommunityRequestsInput{ConversationID: 0}).Validate())
		assertBadRequest(t, (&member_models.GetCommunityRequestsInput{ConversationID: 1, Limit: 101}).Validate())
		require.NoError(t, (&member_models.GetCommunityRequestsInput{ConversationID: 1, Limit: 20}).Validate())

		assertBadRequest(t, (&member_models.GetConversationMembersInput{ConversationID: 0}).Validate())
		assertBadRequest(t, (&member_models.GetConversationMembersInput{ConversationID: 1, Limit: 101}).Validate())
		assertBadRequest(t, (&member_models.GetConversationMembersInput{ConversationID: 1, Offset: -1}).Validate())
		require.NoError(t, (&member_models.GetConversationMembersInput{ConversationID: 1, Limit: 0, Offset: 0}).Validate())
	})
}
