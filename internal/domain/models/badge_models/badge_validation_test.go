package badge_models_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/badge_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
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

func TestBadgeMetadata_Validate(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(24 * time.Hour)
	past := now.Add(-24 * time.Hour)

	var nilMeta *badge_models.BadgeMetadata
	assertBadRequest(t, nilMeta.Validate())

	assertBadRequest(t, (&badge_models.BadgeMetadata{}).Validate())
	assertBadRequest(t, (&badge_models.BadgeMetadata{IssuedAt: now, ExpiresAt: &past}).Validate())

	require.NoError(t, (&badge_models.BadgeMetadata{IssuedAt: now}).Validate())
	require.NoError(t, (&badge_models.BadgeMetadata{IssuedAt: now, ExpiresAt: &future}).Validate())
	require.NoError(t, (&badge_models.BadgeMetadata{IssuedAt: now, ExpiresAt: &now}).Validate())
}

func TestUserBadges_Validate(t *testing.T) {
	now := time.Now().UTC()

	var nilBadges *badge_models.UserBadges
	assertBadRequest(t, nilBadges.Validate())

	empty := badge_models.UserBadges{}
	require.NoError(t, empty.Validate())

	valid := badge_models.UserBadges{
		"certification_federation_francaise": {IssuedAt: now},
	}
	require.NoError(t, valid.Validate())

	blankKey := badge_models.UserBadges{"   ": {IssuedAt: now}}
	assertBadRequest(t, blankKey.Validate())

	tooLongKey := badge_models.UserBadges{strings.Repeat("a", 101): {IssuedAt: now}}
	assertBadRequest(t, tooLongKey.Validate())

	invalidMeta := badge_models.UserBadges{"verified": {}}
	assertBadRequest(t, invalidMeta.Validate())
}
