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

func TestMemberSettingsAndPayload_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

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
		require.NoError(t, (&member_models.MemberSettings{IsMuted: 0, MuteExpireAt: 0, Pinned: 0, RestrictedUntil: 0}).Validate())
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

func TestDefaultMemberSettings(t *testing.T) {
	t.Run("convType 3 (muted by default)", func(t *testing.T) {
		s := member_models.DefaultMemberSettings(3)
		assert.Equal(t, 2, s.IsMuted)
		assert.Equal(t, int64(-1), s.MuteExpireAt)
		assert.Equal(t, -1, s.Pinned)
		assert.True(t, s.MediaAutoDownload)
		assert.Equal(t, int64(0), s.RestrictedUntil)
		require.NoError(t, s.Validate())
	})

	t.Run("default convType", func(t *testing.T) {
		s := member_models.DefaultMemberSettings(variables.ConversationTypeGroup)
		assert.Equal(t, 0, s.IsMuted)
		assert.Equal(t, int64(0), s.MuteExpireAt)
		assert.Equal(t, -1, s.Pinned)
		assert.True(t, s.MediaAutoDownload)
		assert.Equal(t, int64(0), s.RestrictedUntil)
		require.NoError(t, s.Validate())
	})
}
