package feed_service_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/feed_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/feed_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetFeed_Validation(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	_, _, _, err := feed_service.GetFeed(ctx, 100, feed_models.GetFeedInput{LastSeenIndex: -1})
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)

	_, _, _, err = feed_service.GetFeed(ctx, 0, feed_models.GetFeedInput{LastSeenIndex: 0})
	require.Error(t, err)
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)

	keys, err := repoRedis.ScanKeys(ctx, "*", 200)
	require.NoError(t, err)
	assert.Empty(t, keys)
}
