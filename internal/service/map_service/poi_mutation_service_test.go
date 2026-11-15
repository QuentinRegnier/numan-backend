package map_service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

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

func TestPOIMutationServices_ImportSetUpdateDelete(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()
	adminID := int64(9000)

	// 1. ImportPOIsService
	importOut, err := map_service.ImportPOIsService(ctx, adminID, map_models.ImportPOIsInput{
		POIs: []map_models.CreatePOIItemInput{
			{
				ID:             10001,
				Type:           1,
				Title:          "Opéra Garnier",
				Description:    "Palais Garnier",
				Latitude:       48.871944,
				Longitude:      2.331667,
				Address:        "Place de l'Opéra, 75009 Paris",
				Rating:         4.8,
				CommentCount:   210,
				GoogleMapsLink: "https://maps.google.com/?cid=opera",
			},
			{
				Type:         2,
				Title:        "Place Vendôme",
				Description:  "Place historique",
				Latitude:     48.867500,
				Longitude:    2.329444,
				Address:      "Place Vendôme, 75001 Paris",
				Rating:       4.7,
				CommentCount: 85,
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, importOut.ImportedCount)
	require.Len(t, importOut.POIIDs, 2)
	assert.Equal(t, int64(10001), importOut.POIIDs[0])
	assert.Greater(t, importOut.POIIDs[1], int64(0)) // Auto-generated Snowflake ID
	assert.Equal(t, "https://maps.google.com/?cid=opera", importOut.POIs[0].GoogleMapsLink)
	assert.Empty(t, importOut.POIs[0].AppleMapsLink)
	assert.Empty(t, importOut.POIs[1].GoogleMapsLink)
	assert.Empty(t, importOut.POIs[1].AppleMapsLink)

	// 2. SetPOIsService (single item at root)
	setOut, err := map_service.SetPOIsService(ctx, adminID, map_models.SetPOIInput{
		ID:          10003,
		Type:        3,
		Title:       "Centre Pompidou",
		Description: "Musée national d'art moderne",
		Latitude:    48.860642,
		Longitude:   2.352245,
		Address:     "Place Georges-Pompidou, 75004 Paris",
		Rating:      4.6,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, setOut.CreatedCount)
	assert.Equal(t, []int64{10003}, setOut.POIIDs)
	assert.Empty(t, setOut.POIs[0].GoogleMapsLink)

	// Verify all 3 are searchable in BBox
	bboxRes, err := map_service.GetPOIsInBBoxService(ctx, adminID, map_models.GetPoisInBBoxInput{
		SWLat: 48.85,
		SWLng: 2.31,
		NELat: 48.88,
		NELng: 2.36,
	})
	require.NoError(t, err)
	assert.Len(t, bboxRes.POIs, 3)

	// 3. UpdatePOIsService (move coordinates & update title/rating/apple_maps_link)
	newTitle := "Palais Garnier - Opéra National"
	newRating := 4.95
	newLat := 48.872100
	newAppleLink := "https://maps.apple.com/?place-id=opera"
	updOut, err := map_service.UpdatePOIsService(ctx, adminID, map_models.UpdatePOIInput{
		ID:            10001,
		Title:         &newTitle,
		Rating:        &newRating,
		Latitude:      &newLat,
		AppleMapsLink: &newAppleLink,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, updOut.UpdatedCount)
	require.Len(t, updOut.POIs, 1)
	assert.Equal(t, "Palais Garnier - Opéra National", updOut.POIs[0].Title)
	assert.InDelta(t, 4.95, updOut.POIs[0].Rating, 1e-6)
	assert.InDelta(t, 48.872100, updOut.POIs[0].Latitude, 1e-6)
	assert.Equal(t, "https://maps.google.com/?cid=opera", updOut.POIs[0].GoogleMapsLink)
	assert.Equal(t, "https://maps.apple.com/?place-id=opera", updOut.POIs[0].AppleMapsLink)

	// Verify L1 cache was updated synchronously (including MsgPack maps links)
	cached, err := object_cache_service.GetPOIFromObjectCache(ctx, 10001)
	require.NoError(t, err)
	assert.Equal(t, "Palais Garnier - Opéra National", cached.Title)
	assert.Equal(t, "https://maps.google.com/?cid=opera", cached.GoogleMapsLink)
	assert.Equal(t, "https://maps.apple.com/?place-id=opera", cached.AppleMapsLink)
	assert.Equal(t, variables.POIVisibilityPublic, cached.Visibility)

	// Seed a text review for POI 10001 to verify DeletePOIsService purges review ZSET in L1
	require.NoError(t, object_cache_service.AddTextReviewToZSET(ctx, 10001, 77701, 1700000000000))

	// 4. DeletePOIsService (Soft Delete)
	delOut, err := map_service.DeletePOIsService(ctx, adminID, map_models.DeletePOIInput{
		POIIDs: []int64{10001, 10003},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, delOut.DeletedCount)

	// Verify deleted POIs are immediately gone from both Object Cache, GEO BBox search, and review ZSET
	_, err = object_cache_service.GetPOIFromObjectCache(ctx, 10001)
	require.Error(t, err)

	zsetIDs, errZ := object_cache_service.GetPOIReviewIDsFromZSET(ctx, 10001, 0, 10)
	require.NoError(t, errZ)
	assert.Empty(t, zsetIDs)

	bboxAfterDel, err := map_service.GetPOIsInBBoxService(ctx, adminID, map_models.GetPoisInBBoxInput{
		SWLat: 48.85,
		SWLng: 2.31,
		NELat: 48.88,
		NELng: 2.36,
	})
	require.NoError(t, err)
	require.Len(t, bboxAfterDel.POIs, 1)
	assert.Equal(t, "Place Vendôme", bboxAfterDel.POIs[0].Title)

	// Verify Soft Delete payloads (visibility = -1) were enqueued to Redis queue for PostgreSQL
	keys, err := infraRedis.Rdb.Keys(ctx, "request_cache:q:*").Result()
	require.NoError(t, err)
	var softDeletedIDs []int64
	for _, k := range keys {
		items, errL := infraRedis.Rdb.LRange(ctx, k, 0, -1).Result()
		require.NoError(t, errL)
		for _, raw := range items {
			var evt redis.AsyncEvent
			dec := json.NewDecoder(strings.NewReader(raw))
			dec.UseNumber()
			require.NoError(t, dec.Decode(&evt))
			if evt.Type == redis.EntityPOI && evt.Action == redis.ActionDelete {
				payloadMap, ok := evt.Payload.(map[string]any)
				require.True(t, ok)
				assert.Equal(t, json.Number("-1"), payloadMap["visibility"])
				softDeletedIDs = append(softDeletedIDs, evt.ID)
			}
		}
	}
	assert.ElementsMatch(t, []int64{10001, 10003}, softDeletedIDs)
}

func TestPOIMutationServices_ValidationFailures(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	// Empty import
	_, err := map_service.ImportPOIsService(ctx, 1, map_models.ImportPOIsInput{})
	require.Error(t, err)

	// Description > 500 chars
	_, err = map_service.SetPOIsService(ctx, 1, map_models.SetPOIInput{
		Title:       "Test",
		Description: strings.Repeat("a", 501),
		Latitude:    48.85,
		Longitude:   2.35,
	})
	require.Error(t, err)

	// Update non-existent POI
	title := "Ghost"
	_, err = map_service.UpdatePOIsService(ctx, 1, map_models.UpdatePOIInput{
		ID:    99999999,
		Title: &title,
	})
	require.Error(t, err)

	// Update soft-deleted POI must fail with 404 NotFound
	require.NoError(t, object_cache_service.SetPOIInObjectCache(ctx, map_models.POIPayload{
		ID:         88888,
		Type:       1,
		Title:      "Deleted POI",
		Latitude:   48.85,
		Longitude:  2.35,
		Visibility: variables.POIVisibilitySoftDeleted,
	}))
	_, err = map_service.UpdatePOIsService(ctx, 1, map_models.UpdatePOIInput{
		ID:    88888,
		Title: &title,
	})
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)

	// Delete invalid ID
	_, err = map_service.DeletePOIsService(ctx, 1, map_models.DeletePOIInput{
		POIIDs: []int64{-5},
	})
	require.Error(t, err)
}
