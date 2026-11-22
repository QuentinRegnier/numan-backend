package community_recommendation_service_test

import (
	"context"
	"database/sql/driver"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/community_recommendation_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnpinCommunity(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()

	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(1), lite_models.UserLiteRequest{ID: 1, Username: "admin", Grade: variables.UserGradeAdmin}))
	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(2), lite_models.UserLiteRequest{ID: 2, Username: "user", Grade: variables.UserGradeCertified}))

	seedTestCommunity(t, ctx, 1002, "News FR", "fr", 0, false, 80)
	require.NoError(t, cache_service.SetCommunityPinInCache(ctx, "fr", 1, 1002))

	// Non-admin rejected with 403
	_, err := community_recommendation_service.UnpinCommunity(ctx, 2, community_recommendation_models.UnpinCommunityInput{
		ConversationID: 1002,
	})
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus)

	// Unpin pinned community
	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		return test.NewMockRows(
			[]string{"status_code", "conv_lang", "old_pin_slot"},
			[][]driver.Value{{"OK", "fr", int64(1)}},
		), nil
	}, nil)

	unpinOut, err := community_recommendation_service.UnpinCommunity(ctx, 1, community_recommendation_models.UnpinCommunityInput{
		ConversationID: 1002,
	})
	require.NoError(t, err)
	assert.True(t, unpinOut.Unpinned)

	pinsAfterUnpin, err := cache_service.GetCommunityPinsByLang(ctx, "fr")
	require.NoError(t, err)
	assert.Empty(t, pinsAfterUnpin)

	// Idempotent unpin when already unpinned
	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		return test.NewMockRows(
			[]string{"status_code", "conv_lang", "old_pin_slot"},
			[][]driver.Value{{"OK", "fr", nil}},
		), nil
	}, nil)

	idempOut, err := community_recommendation_service.UnpinCommunity(ctx, 1, community_recommendation_models.UnpinCommunityInput{
		ConversationID: 1002,
	})
	require.NoError(t, err)
	assert.False(t, idempOut.Unpinned)

	// Unpin on non-public conversation -> SQL NOT_PUBLIC_COMMUNITY maps to 400 Bad Request
	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		return test.NewMockRows(
			[]string{"status_code", "conv_lang", "old_pin_slot"},
			[][]driver.Value{{"NOT_PUBLIC_COMMUNITY", "", nil}},
		), nil
	}, nil)

	_, err = community_recommendation_service.UnpinCommunity(ctx, 1, community_recommendation_models.UnpinCommunityInput{
		ConversationID: 2001,
	})
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}
