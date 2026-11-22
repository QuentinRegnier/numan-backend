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
	"github.com/QuentinRegnier/numan-backend/internal/service/community_recommendation_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetCommunityPartner(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()

	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(1), lite_models.UserLiteRequest{ID: 1, Username: "admin", Grade: variables.UserGradeAdmin}))
	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(2), lite_models.UserLiteRequest{ID: 2, Username: "user", Grade: variables.UserGradeCertified}))

	seedTestCommunity(t, ctx, 1001, "Tech FR", "fr", 2, false, 100)

	// Non-admin rejected with 403
	_, err := community_recommendation_service.SetCommunityPartner(ctx, 2, community_recommendation_models.SetPartnerInput{
		ConversationID: 1001,
		IsPartner:      true,
	})
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus)

	// SetPartner true -> updates ConvMeta & Object Cache
	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		return test.NewMockRows(
			[]string{"status_code", "conv_lang", "is_partner"},
			[][]driver.Value{{"OK", "fr", true}},
		), nil
	}, nil)

	partnerOut, err := community_recommendation_service.SetCommunityPartner(ctx, 1, community_recommendation_models.SetPartnerInput{
		ConversationID: 1001,
		IsPartner:      true,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1001), partnerOut.ConversationID)
	assert.True(t, partnerOut.IsPartner)

	var meta1001 lite_models.ConvLiteRequest
	require.NoError(t, redis.ConvMeta.GetObject(ctx, int64(1001), &meta1001))
	assert.True(t, meta1001.IsPartner)

	// NOT_ACTIVE_PUBLIC_COMMUNITY -> 400
	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		return test.NewMockRows(
			[]string{"status_code", "conv_lang", "is_partner"},
			[][]driver.Value{{"NOT_ACTIVE_PUBLIC_COMMUNITY", "", false}},
		), nil
	}, nil)

	_, err = community_recommendation_service.SetCommunityPartner(ctx, 1, community_recommendation_models.SetPartnerInput{
		ConversationID: 1001,
		IsPartner:      false,
	})
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
}
