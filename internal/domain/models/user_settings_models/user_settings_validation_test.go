package user_settings_models_test

import (
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
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

func TestUserSettingsNestedAndPayload_Validate(t *testing.T) {
	t.Run("PrivacySettings", func(t *testing.T) {
		var nilP *user_settings_models.PrivacySettings
		assertBadRequest(t, nilP.Validate())
		require.NoError(t, (&user_settings_models.PrivacySettings{
			ProfileVisibility:      variables.ProfileVisibilityFriends,
			PostVisibilityDefault:  variables.PostVisibilityFriend,
			ConversationPermission: 3,
			AddGroupPermission:     2,
			AllowTagging:           variables.TagPermissionNobody,
			AllowMentions:          variables.TagPermissionNobody,
			HideConnections:        variables.ConnectionsVisibilityPrivate,
		}).Validate())

		assertBadRequest(t, (&user_settings_models.PrivacySettings{ProfileVisibility: 3}).Validate())
		assertBadRequest(t, (&user_settings_models.PrivacySettings{PostVisibilityDefault: 3}).Validate())
		assertBadRequest(t, (&user_settings_models.PrivacySettings{ConversationPermission: 4}).Validate())
		assertBadRequest(t, (&user_settings_models.PrivacySettings{AddGroupPermission: 3}).Validate())
		assertBadRequest(t, (&user_settings_models.PrivacySettings{AllowTagging: 4}).Validate())
		assertBadRequest(t, (&user_settings_models.PrivacySettings{AllowMentions: -1}).Validate())
		assertBadRequest(t, (&user_settings_models.PrivacySettings{HideConnections: 4}).Validate())
	})

	t.Run("DisplayAndContentSettings", func(t *testing.T) {
		var nilD *user_settings_models.DisplayAndContentSettings
		assertBadRequest(t, nilD.Validate())
		require.NoError(t, (&user_settings_models.DisplayAndContentSettings{Theme: 2, Language: 1}).Validate())
		assertBadRequest(t, (&user_settings_models.DisplayAndContentSettings{Theme: -1}).Validate())
		assertBadRequest(t, (&user_settings_models.DisplayAndContentSettings{Theme: 3}).Validate())
		assertBadRequest(t, (&user_settings_models.DisplayAndContentSettings{Language: -1}).Validate())
	})

	t.Run("UserSettingsPayload", func(t *testing.T) {
		var nilU *user_settings_models.UserSettingsPayload
		assertBadRequest(t, nilU.Validate())

		valid := user_settings_models.UserSettingsPayload{
			ID:     1,
			UserID: 2,
		}
		require.NoError(t, valid.Validate())

		validVec := make([]float32, variables.VectorDimTotal)
		validWithVec := valid
		validWithVec.TelemetryVector = validVec
		require.NoError(t, validWithVec.Validate())

		badID := valid
		badID.ID = 0
		assertBadRequest(t, badID.Validate())

		badUser := valid
		badUser.UserID = -1
		assertBadRequest(t, badUser.Validate())

		badVecLen := valid
		badVecLen.TelemetryVector = []float32{0.1, 0.2}
		assertBadRequest(t, badVecLen.Validate())

		badVecNaN := valid
		badVecNaN.TelemetryVector = make([]float32, variables.VectorDimTotal)
		badVecNaN.TelemetryVector[0] = float32(math.NaN())
		assertBadRequest(t, badVecNaN.Validate())

		badTs := valid
		badTs.TelemetryTimestamp = -1
		assertBadRequest(t, badTs.Validate())
	})
}

func TestUserSettingsInputs_Validate(t *testing.T) {
	t.Run("CheckUsernameInput", func(t *testing.T) {
		var nilIn *user_settings_models.CheckUsernameInput
		assertBadRequest(t, nilIn.Validate())
		require.NoError(t, (&user_settings_models.CheckUsernameInput{Username: "Alice99"}).Validate())
		assertBadRequest(t, (&user_settings_models.CheckUsernameInput{Username: "ab"}).Validate())
		assertBadRequest(t, (&user_settings_models.CheckUsernameInput{Username: strings.Repeat("a", 31)}).Validate())
		assertBadRequest(t, (&user_settings_models.CheckUsernameInput{Username: "alice_99"}).Validate())
		assertBadRequest(t, (&user_settings_models.CheckUsernameInput{Username: "ééé"}).Validate())
	})

	t.Run("UpdateDisplayInput", func(t *testing.T) {
		var nilIn *user_settings_models.UpdateDisplayInput
		assertBadRequest(t, nilIn.Validate())
		require.NoError(t, (&user_settings_models.UpdateDisplayInput{Theme: 2, Language: 0, SafeForCommute: true}).Validate())
		assertBadRequest(t, (&user_settings_models.UpdateDisplayInput{Theme: 3}).Validate())
		assertBadRequest(t, (&user_settings_models.UpdateDisplayInput{Language: -2}).Validate())
	})

	t.Run("UpdateNotificationsInput", func(t *testing.T) {
		var nilIn *user_settings_models.UpdateNotificationsInput
		assertBadRequest(t, nilIn.Validate())
		require.NoError(t, (&user_settings_models.UpdateNotificationsInput{MasterPushEnabled: true}).Validate())
	})

	t.Run("UpdatePrivacyInput", func(t *testing.T) {
		var nilIn *user_settings_models.UpdatePrivacyInput
		assertBadRequest(t, nilIn.Validate())
		require.NoError(t, (&user_settings_models.UpdatePrivacyInput{
			ProfileVisibility:      2,
			PostVisibilityDefault:  2,
			ConversationPermission: 3,
			AddGroupPermission:     2,
			AllowTagging:           3,
			AllowMentions:          3,
			HideConnections:        3,
		}).Validate())
		assertBadRequest(t, (&user_settings_models.UpdatePrivacyInput{PostVisibilityDefault: 3}).Validate())
	})

	t.Run("UpdateProfileInput", func(t *testing.T) {
		var nilIn *user_settings_models.UpdateProfileInput
		assertBadRequest(t, nilIn.Validate())

		valid := func() user_settings_models.UpdateProfileInput {
			return user_settings_models.UpdateProfileInput{
				Username:  "Alice123",
				Email:     "alice@numan.app",
				Phone:     "+33612345678",
				FirstName: "Alice",
				LastName:  "Dupont",
				Bio:       "Passionnée de sport",
			}
		}
		v := valid()
		require.NoError(t, v.Validate())
		vNoPhone := valid()
		vNoPhone.Phone = ""
		require.NoError(t, vNoPhone.Validate())

		cases := []struct {
			name   string
			mutate func(p *user_settings_models.UpdateProfileInput)
		}{
			{"invalid username", func(p *user_settings_models.UpdateProfileInput) { p.Username = "a_b" }},
			{"invalid email", func(p *user_settings_models.UpdateProfileInput) { p.Email = "bad-email" }},
			{"invalid e164 phone", func(p *user_settings_models.UpdateProfileInput) { p.Phone = "0612345678" }},
			{"blank first name", func(p *user_settings_models.UpdateProfileInput) { p.FirstName = "   " }},
			{"first name > 50 runes", func(p *user_settings_models.UpdateProfileInput) { p.FirstName = strings.Repeat("é", 51) }},
			{"blank last name", func(p *user_settings_models.UpdateProfileInput) { p.LastName = "" }},
			{"last name > 50 runes", func(p *user_settings_models.UpdateProfileInput) { p.LastName = strings.Repeat("é", 51) }},
			{"bio > 500 runes", func(p *user_settings_models.UpdateProfileInput) { p.Bio = strings.Repeat("é", 501) }},
			{"location > 100 runes", func(p *user_settings_models.UpdateProfileInput) { p.Location = strings.Repeat("é", 101) }},
			{"school > 100 runes", func(p *user_settings_models.UpdateProfileInput) { p.School = strings.Repeat("é", 101) }},
			{"work > 100 runes", func(p *user_settings_models.UpdateProfileInput) { p.Work = strings.Repeat("é", 101) }},
			{"negative profile picture id", func(p *user_settings_models.UpdateProfileInput) { p.ProfilePictureID = -1 }},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				p := valid()
				tc.mutate(&p)
				assertBadRequest(t, p.Validate())
			})
		}
	})
}
