package object_cache_service_test

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObjectCachePOIReviewService_Lifecycle(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	review1 := map_models.POIReviewPayload{
		ID:        8001,
		POIID:     5001,
		UserID:    1001,
		Rating:    5,
		Content:   "Magnifique vue !",
		CreatedAt: 1700000001000,
		UpdatedAt: 1700000001000,
	}
	review2 := map_models.POIReviewPayload{
		ID:        8002,
		POIID:     5001,
		UserID:    1002,
		Rating:    4,
		Content:   "Très sympa",
		CreatedAt: 1700000002000,
		UpdatedAt: 1700000002000,
	}

	// 1. Object Cache single & batch
	require.NoError(t, object_cache_service.SetPOIReviewInObjectCache(ctx, review1))
	got1, err := object_cache_service.GetPOIReviewFromObjectCache(ctx, review1.ID)
	require.NoError(t, err)
	assert.Equal(t, review1, got1)

	require.NoError(t, object_cache_service.SetPOIReviewsBatchInObjectCache(ctx, []map_models.POIReviewPayload{review1, review2}))
	found, missing, err := object_cache_service.GetPOIReviewsBatchFromObjectCache(ctx, []int64{8001, 8002, 8003})
	require.NoError(t, err)
	assert.Len(t, found, 2)
	assert.Equal(t, []int64{8003}, missing)

	// 2. User -> Review index
	require.NoError(t, object_cache_service.SetUserPOIReviewID(ctx, 5001, 1001, 8001))
	idxID, err := object_cache_service.GetUserPOIReviewID(ctx, 5001, 1001)
	require.NoError(t, err)
	assert.Equal(t, int64(8001), idxID)

	// 3. ZSET for text reviews (ordered by CreatedAt DESC)
	require.NoError(t, object_cache_service.AddTextReviewToZSET(ctx, 5001, 8001, review1.CreatedAt))
	require.NoError(t, object_cache_service.AddTextReviewToZSET(ctx, 5001, 8002, review2.CreatedAt))

	zsetIDs, err := object_cache_service.GetPOIReviewIDsFromZSET(ctx, 5001, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{8002, 8001}, zsetIDs)

	// 4. Delete from ZSET, index, and Object Cache
	require.NoError(t, object_cache_service.RemoveTextReviewFromZSET(ctx, 5001, 8001))
	zsetAfter, err := object_cache_service.GetPOIReviewIDsFromZSET(ctx, 5001, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{8002}, zsetAfter)

	require.NoError(t, object_cache_service.DeleteUserPOIReviewID(ctx, 5001, 1001))
	_, err = object_cache_service.GetUserPOIReviewID(ctx, 5001, 1001)
	require.Error(t, err)

	require.NoError(t, object_cache_service.DeletePOIReviewFromObjectCache(ctx, 8001))
	_, err = object_cache_service.GetPOIReviewFromObjectCache(ctx, 8001)
	require.Error(t, err)

	// 5. PurgePOIReviewsFromL1 removes all remaining indexed reviews, user indexes, and ZSET for a soft-deleted POI
	require.NoError(t, object_cache_service.SetUserPOIReviewID(ctx, 5001, 1002, 8002))
	object_cache_service.PurgePOIReviewsFromL1(ctx, 5001)

	zsetAfterPurge, err := object_cache_service.GetPOIReviewIDsFromZSET(ctx, 5001, 0, 10)
	require.NoError(t, err)
	assert.Empty(t, zsetAfterPurge)

	_, err = object_cache_service.GetPOIReviewFromObjectCache(ctx, 8002)
	require.Error(t, err)

	_, err = object_cache_service.GetUserPOIReviewID(ctx, 5001, 1002)
	require.Error(t, err)
}
