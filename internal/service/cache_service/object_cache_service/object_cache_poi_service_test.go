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

func TestObjectCachePOIService_Lifecycle(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	poi1 := map_models.POIPayload{
		ID:           5001,
		Type:         1,
		Title:        "Notre-Dame de Paris",
		Description:  "Cathédrale gothique",
		Latitude:     48.852968,
		Longitude:    2.349902,
		Address:      "6 Parvis Notre-Dame, 75004 Paris",
		Rating:       4.9,
		CommentCount: 250,
		MediaIDs:     []int64{11, 12},
	}
	poi2 := map_models.POIPayload{
		ID:           5002,
		Type:         2,
		Title:        "Panthéon",
		Description:  "Monument néo-classique",
		Latitude:     48.846222,
		Longitude:    2.346414,
		Address:      "Place du Panthéon, 75005 Paris",
		Rating:       4.7,
		CommentCount: 95,
		MediaIDs:     []int64{13},
	}

	// 1. Set single POI
	require.NoError(t, object_cache_service.SetPOIInObjectCache(ctx, poi1))

	got1, err := object_cache_service.GetPOIFromObjectCache(ctx, 5001)
	require.NoError(t, err)
	assert.Equal(t, poi1.Title, got1.Title)
	assert.Equal(t, poi1.Latitude, got1.Latitude)

	// 2. Batch Set
	require.NoError(t, object_cache_service.SetPOIsBatchInObjectCache(ctx, []map_models.POIPayload{poi1, poi2}))

	// 3. Batch MGET with 1 missing ID (5003)
	found, missing, err := object_cache_service.GetPOIsBatchFromObjectCache(ctx, []int64{5001, 5002, 5003})
	require.NoError(t, err)
	assert.Len(t, found, 2)
	assert.Equal(t, []int64{5003}, missing)

	// 4. Spatial BBox Search
	bboxIDs, err := object_cache_service.SearchPOIIDsInBBox(ctx, 48.84, 2.33, 48.86, 2.36)
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{5001, 5002}, bboxIDs)

	// 5. Delete POI 5001 from both Object Cache and GEO Index
	require.NoError(t, object_cache_service.DeletePOIsFromObjectCache(ctx, []int64{5001}))

	_, err = object_cache_service.GetPOIFromObjectCache(ctx, 5001)
	require.Error(t, err)

	bboxAfter, err := object_cache_service.SearchPOIIDsInBBox(ctx, 48.84, 2.33, 48.86, 2.36)
	require.NoError(t, err)
	assert.Equal(t, []int64{5002}, bboxAfter)
}
