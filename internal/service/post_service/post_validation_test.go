package post_service_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/post_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertServiceBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestPostServices_InvalidPayloads_NoRedisSideEffects(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	// Seed an existing post to verify UpdatePost/DeletePost also reject invalid inputs before touching it.
	seedPost := post_models.PostPayload{
		ID:         7001,
		UserID:     101,
		Content:    "Original post",
		MediaIDs:   []int64{501},
		Visibility: variables.PostVisibilityAll,
	}
	require.NoError(t, object_cache_service.SetPostInObjectCache(ctx, seedPost))

	initialKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)

	// 1. CreatePost invalid cases (missing media, content > 2200 runes, invalid hashtag, invalid visibility, bad media ID)
	_, err = post_service.CreatePost(ctx, 101, post_models.CreatePostInput{
		Content:    "No media",
		Visibility: variables.PostVisibilityAll,
	})
	assertServiceBadRequest(t, err)

	_, err = post_service.CreatePost(ctx, 101, post_models.CreatePostInput{
		Content:    strings.Repeat("é", 2201),
		MediaIDs:   []int64{501},
		Visibility: variables.PostVisibilityAll,
	})
	assertServiceBadRequest(t, err)

	_, err = post_service.CreatePost(ctx, 101, post_models.CreatePostInput{
		Content:    "Invalid media ID",
		MediaIDs:   []int64{-1},
		Visibility: variables.PostVisibilityAll,
	})
	assertServiceBadRequest(t, err)

	// Invalid callerID triggers PostPayload.Validate() failure only AFTER media activation would fail if media existed,
	// so testing callerID <= 0 cleanly verifies no crash and no Redis enqueueing without seeded media.
	_, err = post_service.CreatePost(ctx, 0, post_models.CreatePostInput{
		Content:    "Valid payload invalid caller",
		MediaIDs:   []int64{501},
		Visibility: variables.PostVisibilityAll,
	})
	require.Error(t, err)

	// 2. UpdatePost invalid cases
	err = post_service.UpdatePost(ctx, 101, post_models.UpdatePostInput{
		PostID:     0,
		Content:    "Update",
		Visibility: variables.PostVisibilityAll,
	})
	assertServiceBadRequest(t, err)

	err = post_service.UpdatePost(ctx, 101, post_models.UpdatePostInput{
		PostID:     7001,
		Content:    strings.Repeat("a", 2201),
		Visibility: variables.PostVisibilityAll,
	})
	assertServiceBadRequest(t, err)

	// 3. DeletePost invalid case
	err = post_service.DeletePost(ctx, 101, post_models.DeletePostInput{PostID: -10})
	assertServiceBadRequest(t, err)

	// 4. GetPosts invalid cases
	_, err = post_service.GetPosts(ctx, 101, post_models.GetPostInput{PostIDs: nil})
	assertServiceBadRequest(t, err)

	_, err = post_service.GetPosts(ctx, 101, post_models.GetPostInput{PostIDs: []int64{7001, 0}})
	assertServiceBadRequest(t, err)

	// 5. GetUserPosts invalid cases
	_, err = post_service.GetUserPosts(ctx, 101, post_models.GetUserPostsInput{TargetUserID: 0})
	assertServiceBadRequest(t, err)

	_, err = post_service.GetUserPosts(ctx, 101, post_models.GetUserPostsInput{TargetUserID: 101, Limit: 101})
	assertServiceBadRequest(t, err)

	_, err = post_service.GetUserPosts(ctx, 101, post_models.GetUserPostsInput{TargetUserID: 101, Offset: -1})
	assertServiceBadRequest(t, err)

	// Verify seedPost wasn't modified and no queues or extra keys were created
	finalKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)
	assert.ElementsMatch(t, initialKeys, finalKeys)

	afterSeed, err := object_cache_service.GetPostFromObjectCache(ctx, 7001)
	require.NoError(t, err)
	assert.Equal(t, "Original post", afterSeed.Content)
	assert.Equal(t, variables.PostVisibilityAll, afterSeed.Visibility)

	queueKeys, err := infraRedis.Rdb.Keys(ctx, "request_cache:q:*").Result()
	require.NoError(t, err)
	assert.Empty(t, queueKeys)
}
