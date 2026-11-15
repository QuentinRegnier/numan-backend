package lite_models_test

import (
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/badge_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
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

func TestLiteModels_Validate(t *testing.T) {
	t.Run("ConversationSettings and ConvLiteRequest", func(t *testing.T) {
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
		bad.Settings.WritePermission = 3
		assertBadRequest(t, bad.Validate())

		bad = valid
		bad.ExternalLink.URL = "ftp://invalid"
		assertBadRequest(t, bad.Validate())
	})

	t.Run("CommunityLiteRequest", func(t *testing.T) {
		valid := lite_models.CommunityLiteRequest{ID: 1, Name: "Go Devs", MemberCount: 5}
		require.NoError(t, valid.Validate())

		var nilComm *lite_models.CommunityLiteRequest
		assertBadRequest(t, nilComm.Validate())
		assertBadRequest(t, (&lite_models.CommunityLiteRequest{ID: 0, Name: "Go"}).Validate())
		assertBadRequest(t, (&lite_models.CommunityLiteRequest{ID: 1, Name: "  "}).Validate())
		assertBadRequest(t, (&lite_models.CommunityLiteRequest{ID: 1, Name: "Go", MemberCount: -1}).Validate())
	})

	t.Run("MemberSettingsLite and MemberLiteRequest", func(t *testing.T) {
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
		assertBadRequest(t, (&lite_models.MemberLiteRequest{ConversationID: 1, UserID: 2, Role: 99}).Validate())
		assertBadRequest(t, (&lite_models.MemberLiteRequest{ConversationID: 1, UserID: 2, Settings: lite_models.MemberSettingsLite{Pinned: 99}}).Validate())
	})

	t.Run("TagPostItem", func(t *testing.T) {
		require.NoError(t, (&lite_models.TagPostItem{PostID: 10}).Validate())
		var nilTag *lite_models.TagPostItem
		assertBadRequest(t, nilTag.Validate())
		assertBadRequest(t, (&lite_models.TagPostItem{PostID: 0}).Validate())
	})

	t.Run("TelemetryProfileLite", func(t *testing.T) {
		valid := lite_models.TelemetryProfileLite{
			Vector:          make([]float32, variables.VectorDimTotal),
			TopTags:         []string{"go"},
			ConfidenceScore: 0.75,
			TimestampMs:     100,
		}
		require.NoError(t, valid.Validate())

		var nilTel *lite_models.TelemetryProfileLite
		assertBadRequest(t, nilTel.Validate())
		assertBadRequest(t, (&lite_models.TelemetryProfileLite{Vector: []float32{0.1}}).Validate())
		assertBadRequest(t, (&lite_models.TelemetryProfileLite{ConfidenceScore: math.NaN()}).Validate())
		assertBadRequest(t, (&lite_models.TelemetryProfileLite{TimestampMs: -1}).Validate())
	})

	t.Run("UserLiteRequest", func(t *testing.T) {
		valid := lite_models.UserLiteRequest{
			ID:         10,
			Username:   "alice",
			Grade:      variables.UserGradeUser,
			TrustScore: 0.5,
			Badges:     lite_models.UserBadges{"certified": badge_models.BadgeMetadata{IssuedAt: time.Now()}},
		}
		require.NoError(t, valid.Validate())

		var nilUser *lite_models.UserLiteRequest
		assertBadRequest(t, nilUser.Validate())
		bad := valid
		bad.ID = 0
		assertBadRequest(t, bad.Validate())

		bad = valid
		bad.Username = " "
		assertBadRequest(t, bad.Validate())

		bad = valid
		bad.TrustScore = 1.5
		assertBadRequest(t, bad.Validate())

		bad = valid
		bad.AllowMentions = 9
		assertBadRequest(t, bad.Validate())
	})
}
