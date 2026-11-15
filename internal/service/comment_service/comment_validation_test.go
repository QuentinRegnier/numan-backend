package comment_service_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/comment_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/comment_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
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

func TestCommentServices_InvalidPayloads_NoRedisSideEffects(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	seedPost := post_models.PostPayload{
		ID:         8001,
		UserID:     201,
		Content:    "Parent post",
		Visibility: variables.PostVisibilityAll,
	}
	require.NoError(t, object_cache_service.SetPostInObjectCache(ctx, seedPost))

	initialKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)

	// 1. CreateComment invalid cases
	assertBadRequest(t, comment_service.CreateComment(ctx, 202, comment_models.CreateCommentInput{
		PostID:  0,
		Content: "Valid content",
	}))
	assertBadRequest(t, comment_service.CreateComment(ctx, 202, comment_models.CreateCommentInput{
		PostID:  8001,
		Content: "   ",
	}))
	assertBadRequest(t, comment_service.CreateComment(ctx, 202, comment_models.CreateCommentInput{
		PostID:  8001,
		Content: strings.Repeat("a", 2201),
	}))
	// Invalid callerID rejected by CommentPayload.Validate() before touching L1 ZSET or EnqueueDB
	assertBadRequest(t, comment_service.CreateComment(ctx, 0, comment_models.CreateCommentInput{
		PostID:  8001,
		Content: "Valid content",
	}))

	// 2. UpdateComment invalid cases
	assertBadRequest(t, comment_service.UpdateComment(ctx, 202, comment_models.UpdateCommentInput{
		CommentID: 0,
		Content:   "Updated",
	}))
	assertBadRequest(t, comment_service.UpdateComment(ctx, 202, comment_models.UpdateCommentInput{
		CommentID: 123,
		Content:   strings.Repeat("é", 2201),
	}))

	// 3. DeleteComment invalid cases
	assertBadRequest(t, comment_service.DeleteComment(ctx, 202, comment_models.DeleteCommentInput{
		CommentID: -1,
	}))

	// 4. GetComments invalid cases
	_, err = comment_service.GetComments(ctx, 202, comment_models.GetCommentsInput{PostID: 0})
	assertBadRequest(t, err)
	_, err = comment_service.GetComments(ctx, 202, comment_models.GetCommentsInput{PostID: 8001, Offset: -1})
	assertBadRequest(t, err)
	_, err = comment_service.GetComments(ctx, 202, comment_models.GetCommentsInput{PostID: 8001, Limit: 101})
	assertBadRequest(t, err)

	// Ensure zero Redis side effects occurred
	finalKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)
	assert.ElementsMatch(t, initialKeys, finalKeys)

	afterPost, err := object_cache_service.GetPostFromObjectCache(ctx, 8001)
	require.NoError(t, err)
	assert.Equal(t, 0, afterPost.CommentCount)

	queueKeys, err := infraRedis.Rdb.Keys(ctx, "request_cache:q:*").Result()
	require.NoError(t, err)
	assert.Empty(t, queueKeys)
}

func TestCreateComment_ValidInput_UpdatesCacheAndEnqueues(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	seedPost := post_models.PostPayload{
		ID:         8002,
		UserID:     201,
		Content:    "Parent post",
		Visibility: variables.PostVisibilityAll,
	}
	require.NoError(t, object_cache_service.SetPostInObjectCache(ctx, seedPost))

	err := comment_service.CreateComment(ctx, 202, comment_models.CreateCommentInput{
		PostID:  8002,
		Content: "Très bon post !",
	})
	require.NoError(t, err)

	afterPost, err := object_cache_service.GetPostFromObjectCache(ctx, 8002)
	require.NoError(t, err)
	assert.Equal(t, 1, afterPost.CommentCount)

	queueKeys, err := infraRedis.Rdb.Keys(ctx, "request_cache:q:*").Result()
	require.NoError(t, err)
	assert.NotEmpty(t, queueKeys)
}
