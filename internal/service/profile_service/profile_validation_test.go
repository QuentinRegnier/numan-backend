package profile_service_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/profile_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/profile_service"
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

func TestGetProfile_InvalidPayloads_NoRedisSideEffects(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	initialKeys, err := repoRedis.ScanKeys(ctx, "*", 200)
	require.NoError(t, err)

	_, err = profile_service.GetProfile(ctx, 0, profile_models.GetProfileInput{})
	assertBadRequest(t, err)

	_, err = profile_service.GetProfile(ctx, 1, profile_models.GetProfileInput{TargetID: -5})
	assertBadRequest(t, err)

	_, err = profile_service.GetProfile(ctx, 1, profile_models.GetProfileInput{Limit: -1})
	assertBadRequest(t, err)

	_, err = profile_service.GetProfile(ctx, 1, profile_models.GetProfileInput{Limit: 51})
	assertBadRequest(t, err)

	_, err = profile_service.GetProfile(ctx, 1, profile_models.GetProfileInput{Offset: -1})
	assertBadRequest(t, err)

	finalKeys, err := repoRedis.ScanKeys(ctx, "*", 200)
	require.NoError(t, err)
	assert.ElementsMatch(t, initialKeys, finalKeys)
}
