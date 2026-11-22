package community_recommendation_service_test

import (
	"context"
	"database/sql/driver"
	"net/http"
	"strings"
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

func TestPinCommunity(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()

	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(1), lite_models.UserLiteRequest{ID: 1, Username: "admin", Grade: variables.UserGradeAdmin}))
	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(2), lite_models.UserLiteRequest{ID: 2, Username: "cert", Grade: variables.UserGradeCertified}))

	seedTestCommunity(t, ctx, 1001, "Tech FR", "fr", 2, false, 100)
	seedTestCommunity(t, ctx, 1002, "News FR", "fr", 0, false, 80)

	// Non-admin rejected with 403
	_, err := community_recommendation_service.PinCommunity(ctx, 2, community_recommendation_models.PinCommunityInput{
		ConversationID: 1001,
		Slot:           1,
	})
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus)

	// 1. Pin success on slot 1
	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		if strings.Contains(query, "func_set_community_pin") {
			return test.NewMockRows(
				[]string{"status_code", "conv_lang", "old_pin_slot", "displaced_conv_id"},
				[][]driver.Value{{"OK", "fr", nil, nil}},
			), nil
		}
		return test.NewMockRows([]string{"id"}, nil), nil
	}, nil)

	pinOut, err := community_recommendation_service.PinCommunity(ctx, 1, community_recommendation_models.PinCommunityInput{
		ConversationID: 1001,
		Slot:           1,
	})
	require.NoError(t, err)
	assert.Equal(t, "fr", pinOut.Lang)
	assert.Equal(t, 1, pinOut.PinSlot)
	assert.Equal(t, int64(0), pinOut.DisplacedConversationID)

	pins, err := cache_service.GetCommunityPinsByLang(ctx, "fr")
	require.NoError(t, err)
	assert.Equal(t, int64(1001), pins[1])

	var meta1001 lite_models.ConvLiteRequest
	require.NoError(t, redis.ConvMeta.GetObject(ctx, int64(1001), &meta1001))
	assert.Equal(t, 1, meta1001.PinSlot)

	// 2. Pin conflict on slot 1 when force_replace=false -> 409
	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		return test.NewMockRows(
			[]string{"status_code", "conv_lang", "old_pin_slot", "displaced_conv_id"},
			[][]driver.Value{{"CONFLICT", "fr", nil, int64(1001)}},
		), nil
	}, nil)

	_, err = community_recommendation_service.PinCommunity(ctx, 1, community_recommendation_models.PinCommunityInput{
		ConversationID: 1002,
		Slot:           1,
		ForceReplace:   false,
	})
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusConflict, appErr.HTTPStatus)

	// 3. Pin with force_replace=true displaces 1001 and clears its PinSlot in L1
	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		return test.NewMockRows(
			[]string{"status_code", "conv_lang", "old_pin_slot", "displaced_conv_id"},
			[][]driver.Value{{"OK", "fr", nil, int64(1001)}},
		), nil
	}, nil)

	pinReplaceOut, err := community_recommendation_service.PinCommunity(ctx, 1, community_recommendation_models.PinCommunityInput{
		ConversationID: 1002,
		Slot:           1,
		ForceReplace:   true,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1001), pinReplaceOut.DisplacedConversationID)

	pinsAfter, err := cache_service.GetCommunityPinsByLang(ctx, "fr")
	require.NoError(t, err)
	assert.Equal(t, int64(1002), pinsAfter[1])

	require.NoError(t, redis.ConvMeta.GetObject(ctx, int64(1001), &meta1001))
	assert.Equal(t, 0, meta1001.PinSlot)
}
