package saved_service_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/saved_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/saved_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertSavedBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestSavedServices_InvalidPayloads_NoRedisSideEffects(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	seedPost := post_models.PostPayload{
		ID:         9200,
		UserID:     401,
		Content:    "Saveable post",
		Visibility: variables.PostVisibilityAll,
	}
	require.NoError(t, object_cache_service.SetPostInObjectCache(ctx, seedPost))

	initialKeys, err := repoRedis.ScanKeys(ctx, "*", 200)
	require.NoError(t, err)

	// Invalid userID, postID, or action must fail with BadRequest BEFORE any Redis ZSET or queue write
	// and invalid action on a valid post must return 400 BadRequest (not 404 NotFound).
	assertSavedBadRequest(t, saved_service.ToggleSaved(ctx, 0, 9200, variables.SavedActionSave))
	assertSavedBadRequest(t, saved_service.ToggleSaved(ctx, 402, 0, variables.SavedActionSave))
	assertSavedBadRequest(t, saved_service.ToggleSaved(ctx, 402, 999999, "invalid_action"))
	assertSavedBadRequest(t, saved_service.ToggleSaved(ctx, 402, 9200, "invalid_action"))

	// GetSavedPosts invalid inputs
	_, err = saved_service.GetSavedPosts(ctx, 0, saved_models.GetSavedInput{Limit: 10})
	assertSavedBadRequest(t, err)
	_, err = saved_service.GetSavedPosts(ctx, 402, saved_models.GetSavedInput{Offset: -1})
	assertSavedBadRequest(t, err)
	_, err = saved_service.GetSavedPosts(ctx, 402, saved_models.GetSavedInput{Limit: 101})
	assertSavedBadRequest(t, err)

	finalKeys, err := repoRedis.ScanKeys(ctx, "*", 200)
	require.NoError(t, err)
	assert.ElementsMatch(t, initialKeys, finalKeys)

	queueKeys, err := repoRedis.ScanKeys(ctx, "request_cache:q:*", 200)
	require.NoError(t, err)
	assert.Empty(t, queueKeys)
}

func TestToggleSaved_ValidAndNotFound(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	// Valid action on missing post returns 404 NotFound
	err := saved_service.ToggleSaved(ctx, 402, 999999, variables.SavedActionSave)
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)

	// Valid action on cached post updates ZSET and enqueues
	require.NoError(t, object_cache_service.SetPostInObjectCache(ctx, post_models.PostPayload{
		ID:         9201,
		UserID:     401,
		Visibility: variables.PostVisibilityAll,
	}))
	require.NoError(t, saved_service.ToggleSaved(ctx, 402, 9201, variables.SavedActionSave))

	ids, err := object_cache_service.GetSavedPostIDs(ctx, 402, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{9201}, ids)

	queueKeys, err := repoRedis.ScanKeys(ctx, "request_cache:q:*", 200)
	require.NoError(t, err)
	assert.NotEmpty(t, queueKeys)
}
