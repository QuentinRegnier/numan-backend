package report_service

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/report_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubmitReport_WeightedImpactAndPreventiveShadowban(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	// Seed VIP (0.90) and Newbie (0.20)
	vip := lite_models.UserLiteRequest{ID: 1, Username: "vip", TrustScore: 0.90}
	newbie := lite_models.UserLiteRequest{ID: 2, Username: "newbie", TrustScore: 0.20}
	require.NoError(t, redis.UsersLite.SetObject(ctx, vip.ID, vip))
	require.NoError(t, redis.UsersLite.SetObject(ctx, newbie.ID, newbie))

	// Seed a public post (Visibility = 0) authored by Newbie
	post := post_models.PostPayload{
		ID:         500,
		UserID:     newbie.ID,
		Content:    "Suspect post",
		Visibility: variables.PostVisibilityAll,
	}
	require.NoError(t, object_cache_service.SetPostInObjectCache(ctx, post))

	// VIP signals Newbie's post -> Impact = 0.90 - 0.20 = 0.70 (> 0.40) -> Immediate Shadowban (Visibility = 3)
	err := SubmitReport(ctx, vip.ID, report_models.CreateReportInput{
		TargetType: variables.ReportTargetPost,
		TargetIDs:  []int64{500},
		Category:   variables.ReportCatIllegalContent,
		Reason:     "Contenu interdit",
	})
	require.NoError(t, err)

	updatedPost, errCache := object_cache_service.GetPostFromObjectCache(ctx, 500)
	require.NoError(t, errCache)
	assert.Equal(t, variables.PostVisibilityQuarantine, updatedPost.Visibility)
}
