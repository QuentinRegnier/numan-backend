package user_settings_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdatePrivacyInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

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
}
