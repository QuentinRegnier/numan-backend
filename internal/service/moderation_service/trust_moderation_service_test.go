package moderation_service

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReviewQuarantineMedia_ApproveAndReject(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	// Seed moderator (Grade 3) and normal user (Grade 0)
	require.NoError(t, redis.UsersLite.SetObject(ctx, 99, lite_models.UserLiteRequest{ID: 99, Username: "mod", Grade: variables.UserGradeModerator, TrustScore: 0.95}))
	require.NoError(t, redis.UsersLite.SetObject(ctx, 10, lite_models.UserLiteRequest{ID: 10, Username: "user", Grade: variables.UserGradeUser, TrustScore: 0.20}))

	// Non-moderator rejected with 403 Forbidden
	_, errForbidden := ReviewQuarantineMedia(ctx, 10, moderation_models.ReviewQuarantineInput{
		MediaID:  50,
		Decision: "approve",
	})
	require.Error(t, errForbidden)

	// Setup Postgres mock for media 50 (visibility = 2 quarantine)
	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		if strings.Contains(query, "content.get_media") {
			return test.NewMockRows(
				[]string{"id", "owner_id", "storage_path", "visibility", "created_at", "updated_at"},
				[][]driver.Value{{int64(50), int64(10), "uploads/pic.avif", int64(2), now, now}},
			), nil
		}
		return test.NewMockRows([]string{}, nil), nil
	}, nil)

	out, err := ReviewQuarantineMedia(ctx, 99, moderation_models.ReviewQuarantineInput{
		MediaID:  50,
		Decision: "approve",
		Reason:   "Photo conforme",
	})
	require.NoError(t, err)
	assert.Equal(t, variables.MediaVisibilityPublic, out.NewVisibility)
	assert.Equal(t, variables.TrustEventQuarantineApproved, out.TrustEvent)
}

func TestApplyPreventiveShadowbanToPost(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	postID := int64(888)
	authorID := int64(999)

	post := post_models.PostPayload{
		ID:         postID,
		UserID:     authorID,
		Content:    "Flagged post",
		Visibility: variables.PostVisibilityAll,
	}
	require.NoError(t, object_cache_service.SetPostInObjectCache(ctx, post))

	ApplyPreventiveShadowbanToPost(ctx, postID)

	updated, err := object_cache_service.GetPostFromObjectCache(ctx, postID)
	require.NoError(t, err)
	assert.Equal(t, variables.PostVisibilityQuarantine, updated.Visibility)
}
