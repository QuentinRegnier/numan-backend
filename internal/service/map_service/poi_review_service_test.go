package map_service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/map_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPOIReviewService_UnifiedRatingTextFilterUpdateDelete(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	// Seed POI in L1
	require.NoError(t, object_cache_service.SetPOIInObjectCache(ctx, map_models.POIPayload{
		ID:        5001,
		Type:      1,
		Title:     "Louvre",
		Latitude:  48.860611,
		Longitude: 2.337644,
		Address:   "Rue de Rivoli, Paris",
	}))

	// Seed user profiles in SpeedCache L1
	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(1001), lite_models.UserLiteRequest{ID: 1001, Username: "alice"}))
	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(1002), lite_models.UserLiteRequest{ID: 1002, Username: "bob"}))

	// 1. User 1001 posts a rating ONLY (empty content)
	out1, err := map_service.CreatePOIReviewService(ctx, 1001, map_models.CreatePOIReviewInput{
		POIID:   5001,
		Rating:  5,
		Content: "",
	})
	require.NoError(t, err)
	assert.False(t, out1.IsUpdate)
	assert.InDelta(t, 5.0, out1.POIRating, 0.0001)
	assert.Equal(t, 1, out1.POICommentCount)
	assert.Equal(t, "", out1.Review.Content)
	assert.Equal(t, "alice", out1.Review.AuthorUsername)

	// Verify pure rating is NOT returned by GetPOIReviewsService
	getOut1, err := map_service.GetPOIReviewsService(ctx, 1001, map_models.GetPOIReviewsInput{POIID: 5001, Limit: 20, Offset: 0})
	require.NoError(t, err)
	assert.Empty(t, getOut1.Reviews)

	// 2. User 1002 posts a text review (rating=3)
	out2, err := map_service.CreatePOIReviewService(ctx, 1002, map_models.CreatePOIReviewInput{
		POIID:   5001,
		Rating:  3,
		Content: "Beaucoup de monde",
	})
	require.NoError(t, err)
	assert.False(t, out2.IsUpdate)
	assert.InDelta(t, 4.0, out2.POIRating, 0.0001) // (5 + 3) / 2 = 4.0
	assert.Equal(t, 2, out2.POICommentCount)

	// GetPOIReviewsService returns ONLY User 1002's text review
	getOut2, err := map_service.GetPOIReviewsService(ctx, 1001, map_models.GetPOIReviewsInput{POIID: 5001})
	require.NoError(t, err)
	require.Len(t, getOut2.Reviews, 1)
	assert.Equal(t, "Beaucoup de monde", getOut2.Reviews[0].Content)
	assert.Equal(t, "bob", getOut2.Reviews[0].AuthorUsername)

	// 3. User 1001 updates their rating (5 -> 1) and adds text (1 review per user constraint)
	out1Upd, err := map_service.CreatePOIReviewService(ctx, 1001, map_models.CreatePOIReviewInput{
		POIID:   5001,
		Rating:  1,
		Content: "Trop cher finalement",
	})
	require.NoError(t, err)
	assert.True(t, out1Upd.IsUpdate)
	assert.Equal(t, out1.Review.ID, out1Upd.Review.ID)
	assert.InDelta(t, 2.0, out1Upd.POIRating, 0.0001) // (1 + 3) / 2 = 2.0
	assert.Equal(t, 2, out1Upd.POICommentCount)       // Count stays 2!

	// Now both text reviews are returned
	getOut3, err := map_service.GetPOIReviewsService(ctx, 1001, map_models.GetPOIReviewsInput{POIID: 5001})
	require.NoError(t, err)
	require.Len(t, getOut3.Reviews, 2)

	// 4. User 1001 deletes their review (Soft Delete) -> average returns cleanly to 3.0, count=1
	delOut, err := map_service.DeletePOIReviewService(ctx, 1001, map_models.DeletePOIReviewInput{POIID: 5001})
	require.NoError(t, err)
	assert.True(t, delOut.Deleted)
	assert.InDelta(t, 3.0, delOut.POIRating, 0.0001)
	assert.Equal(t, 1, delOut.POICommentCount)

	// Verify only User 1002 remains in GetPOIReviewsService
	getOut4, err := map_service.GetPOIReviewsService(ctx, 1001, map_models.GetPOIReviewsInput{POIID: 5001})
	require.NoError(t, err)
	require.Len(t, getOut4.Reviews, 1)
	assert.Equal(t, int64(1002), getOut4.Reviews[0].UserID)

	// 5. Verify EntityPOIReview events were enqueued cleanly to Redis (and NOT EntityPOI full updates)
	keys, err := infraRedis.Rdb.Keys(ctx, "request_cache:q:*").Result()
	require.NoError(t, err)
	var reviewEventCount, reviewSoftDeleteCount, poiFullUpdateCount int
	for _, k := range keys {
		items, errL := infraRedis.Rdb.LRange(ctx, k, 0, -1).Result()
		require.NoError(t, errL)
		for _, raw := range items {
			var evt redis.AsyncEvent
			dec := json.NewDecoder(strings.NewReader(raw))
			dec.UseNumber()
			require.NoError(t, dec.Decode(&evt))
			if evt.Type == redis.EntityPOIReview {
				reviewEventCount++
				if evt.Action == redis.ActionDelete {
					reviewSoftDeleteCount++
					payloadMap, ok := evt.Payload.(map[string]any)
					require.True(t, ok)
					assert.Equal(t, json.Number("-1"), payloadMap["visibility"])
				}
			} else if evt.Type == redis.EntityPOI {
				poiFullUpdateCount++
			}
		}
	}
	assert.Equal(t, 4, reviewEventCount)      // 3 creates/updates + 1 soft delete
	assert.Equal(t, 1, reviewSoftDeleteCount) // 1 soft delete with visibility=-1
	assert.Equal(t, 0, poiFullUpdateCount)    // NEVER update entire POIPayload in DB!

	// 6. Error cases
	_, errNotFound := map_service.DeletePOIReviewService(ctx, 1001, map_models.DeletePOIReviewInput{POIID: 5001})
	var appErr1 *numan_error.AppError
	require.ErrorAs(t, errNotFound, &appErr1)
	assert.Equal(t, http.StatusNotFound, appErr1.HTTPStatus)

	_, errBadPOI := map_service.CreatePOIReviewService(ctx, 1001, map_models.CreatePOIReviewInput{POIID: 999999, Rating: 5})
	var appErr2 *numan_error.AppError
	require.ErrorAs(t, errBadPOI, &appErr2)
	assert.Equal(t, http.StatusNotFound, appErr2.HTTPStatus)

	// Soft-deleted POI cannot be reviewed or fetched for reviews
	require.NoError(t, object_cache_service.SetPOIInObjectCache(ctx, map_models.POIPayload{
		ID:         5099,
		Type:       1,
		Title:      "POI Supprimé",
		Latitude:   48.86,
		Longitude:  2.33,
		Visibility: variables.POIVisibilitySoftDeleted,
	}))
	_, errDeletedPOIReview := map_service.CreatePOIReviewService(ctx, 1001, map_models.CreatePOIReviewInput{POIID: 5099, Rating: 5})
	var appErr3 *numan_error.AppError
	require.ErrorAs(t, errDeletedPOIReview, &appErr3)
	assert.Equal(t, http.StatusNotFound, appErr3.HTTPStatus)

	_, errDeletedPOIGet := map_service.GetPOIReviewsService(ctx, 1001, map_models.GetPOIReviewsInput{POIID: 5099})
	var appErr4 *numan_error.AppError
	require.ErrorAs(t, errDeletedPOIGet, &appErr4)
	assert.Equal(t, http.StatusNotFound, appErr4.HTTPStatus)
}
