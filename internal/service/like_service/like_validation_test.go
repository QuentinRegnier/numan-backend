package like_service_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/like_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/like_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertLikeBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestLikeServices_InvalidPayloads_NoRedisSideEffects(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	seedPost := post_models.PostPayload{
		ID:         9100,
		UserID:     301,
		Content:    "Likeable post",
		Visibility: variables.PostVisibilityAll,
	}
	require.NoError(t, object_cache_service.SetPostInObjectCache(ctx, seedPost))

	initialKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)
	initialQueueKeys, err := infraRedis.Rdb.Keys(ctx, "request_cache:q:*").Result()
	require.NoError(t, err)

	// 1. TogglePostLike invalid payloads & invalid caller
	assertLikeBadRequest(t, like_service.TogglePostLike(ctx, 302, like_models.LikePostInput{PostID: 0, Action: "like"}))
	assertLikeBadRequest(t, like_service.TogglePostLike(ctx, 302, like_models.LikePostInput{PostID: 9100, Action: "bogus"}))
	assertLikeBadRequest(t, like_service.TogglePostLike(ctx, 0, like_models.LikePostInput{PostID: 9100, Action: "like"}))

	// 2. ToggleCommentLike invalid payloads & invalid caller
	assertLikeBadRequest(t, like_service.ToggleCommentLike(ctx, 302, like_models.LikeCommentInput{CommentID: -1, Action: "like"}))
	assertLikeBadRequest(t, like_service.ToggleCommentLike(ctx, 302, like_models.LikeCommentInput{CommentID: 10, Action: "other"}))
	assertLikeBadRequest(t, like_service.ToggleCommentLike(ctx, -3, like_models.LikeCommentInput{CommentID: 10, Action: "like"}))

	// 3. GetPostLikes invalid pagination & post_id
	_, err = like_service.GetPostLikes(ctx, like_models.GetPostLikesInput{CallerID: 302, PostID: 0})
	assertLikeBadRequest(t, err)
	_, err = like_service.GetPostLikes(ctx, like_models.GetPostLikesInput{CallerID: 302, PostID: 9100, Offset: -1})
	assertLikeBadRequest(t, err)
	_, err = like_service.GetPostLikes(ctx, like_models.GetPostLikesInput{CallerID: 302, PostID: 9100, Limit: 101})
	assertLikeBadRequest(t, err)

	// Verify zero idempotency keys, counter mutations, or queued events
	finalKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)
	assert.ElementsMatch(t, initialKeys, finalKeys)

	afterPost, err := object_cache_service.GetPostFromObjectCache(ctx, 9100)
	require.NoError(t, err)
	assert.Equal(t, 0, afterPost.LikeCount)

	queueKeys, err := infraRedis.Rdb.Keys(ctx, "request_cache:q:*").Result()
	require.NoError(t, err)
	assert.ElementsMatch(t, initialQueueKeys, queueKeys)
}
