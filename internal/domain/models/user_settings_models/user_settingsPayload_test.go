package user_settings_models_test

import (
	"math"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserSettingsNestedAndPayload_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

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

	t.Run("NotificationSettings", func(t *testing.T) {
		var nilN *user_settings_models.NotificationSettings
		assertBadRequest(t, nilN.Validate())
		require.NoError(t, (&user_settings_models.NotificationSettings{MasterPushEnabled: true}).Validate())
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
