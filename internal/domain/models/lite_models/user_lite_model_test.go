package lite_models_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/badge_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserLiteRequest_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

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
}
