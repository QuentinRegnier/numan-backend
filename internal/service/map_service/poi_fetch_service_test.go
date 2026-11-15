package map_service_test

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/map_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPOIsInBBoxService_L1HitAndMediaResolution(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	// Seed public media 701 and quarantined media 702 in L1 Media Object Cache
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, media_models.MediaPayload{
		ID:          701,
		OwnerID:     1,
		StoragePath: "pois/eiffel.avif",
		Visibility:  variables.MediaVisibilityPublic,
	}))
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, media_models.MediaPayload{
		ID:          702,
		OwnerID:     1,
		StoragePath: "pois/quarantine.avif",
		Visibility:  variables.MediaVisibilityQuarantine,
	}))

	// Seed 2 POIs in Paris and 1 in Lyon
	poi1 := map_models.POIPayload{
		ID:             1001,
		Type:           1,
		Title:          "Tour Eiffel",
		Description:    "Dame de fer",
		Latitude:       48.858370,
		Longitude:      2.294481,
		Address:        "Champ de Mars, Paris",
		Rating:         4.9,
		CommentCount:   400,
		MediaIDs:       []int64{701, 702},
		GoogleMapsLink: "https://maps.google.com/?cid=eiffel",
		AppleMapsLink:  "https://maps.apple.com/?place-id=eiffel",
	}
	poi2 := map_models.POIPayload{
		ID:           1002,
		Type:         2,
		Title:        "Musée du Louvre",
		Description:  "Pyramide du Louvre",
		Latitude:     48.860611,
		Longitude:    2.337644,
		Address:      "Rue de Rivoli, Paris",
		Rating:       4.8,
		CommentCount: 350,
		MediaIDs:     []int64{701},
	}
	poiLyon := map_models.POIPayload{
		ID:           2001,
		Type:         1,
		Title:        "Place Bellecour",
		Description:  "Centre de Lyon",
		Latitude:     45.757814,
		Longitude:    4.832011,
		Address:      "69002 Lyon",
		Rating:       4.5,
		CommentCount: 80,
	}

	require.NoError(t, object_cache_service.SetPOIsBatchInObjectCache(ctx, []map_models.POIPayload{poi1, poi2, poiLyon}))

	out, err := map_service.GetPOIsInBBoxService(ctx, 999, map_models.GetPoisInBBoxInput{
		SWLat: 48.80,
		SWLng: 2.25,
		NELat: 48.90,
		NELng: 2.40,
	})
	require.NoError(t, err)
	require.Len(t, out.POIs, 2)

	// Ordered by Rating DESC (4.9 then 4.8)
	assert.Equal(t, int64(1001), out.POIs[0].ID)
	assert.Equal(t, int64(1002), out.POIs[1].ID)

	// Only public media 701 is resolved; quarantined media 702 is filtered out
	require.Len(t, out.POIs[0].Media, 1)
	assert.Equal(t, int64(701), out.POIs[0].Media[0].MediaID)
	assert.Contains(t, out.POIs[0].Media[0].URL, "pois/eiffel.avif")

	// Provided links respected; omitted links remain empty
	assert.Equal(t, "https://maps.google.com/?cid=eiffel", out.POIs[0].GoogleMapsLink)
	assert.Equal(t, "https://maps.apple.com/?place-id=eiffel", out.POIs[0].AppleMapsLink)
	assert.Empty(t, out.POIs[1].GoogleMapsLink)
	assert.Empty(t, out.POIs[1].AppleMapsLink)
}

func TestGetPOIsInBBoxService_L3SelfHealingOnObjectMissAndColdGeo(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()
	now := domain.NowMillis()

	cols := []string{
		"id", "type", "title", "description", "latitude", "longitude", "address",
		"phone", "website", "rating", "comment_count", "media_ids",
		"google_maps_link", "apple_maps_link", "visibility", "created_at", "updated_at",
	}

	// Case A: GEO has ID 3001, but Object Cache misses -> self-heals via FuncGetPOIsByIDs
	require.NoError(t, redis.POIsGeo.GeoAdd(ctx, object_cache_service.GlobalPOIGeoIndexKey, 2.3522, 48.8566, 3001))

	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		assert.Contains(t, query, "content.func_get_pois_by_ids")
		return test.NewMockRows(cols, [][]driver.Value{
			{
				int64(3001), int64(1), "Hôtel de Ville", "Mairie de Paris", 48.8566, 2.3522, "Place de l'Hôtel de Ville",
				nil, nil, 4.6, int64(42), "{}", "https://maps.google.com/?cid=hdv", nil, int64(0), now, now,
			},
		}), nil
	}, nil)

	out, err := map_service.GetPOIsInBBoxService(ctx, 100, map_models.GetPoisInBBoxInput{
		SWLat: 48.84,
		SWLng: 2.33,
		NELat: 48.87,
		NELng: 2.37,
	})
	require.NoError(t, err)
	require.Len(t, out.POIs, 1)
	assert.Equal(t, int64(3001), out.POIs[0].ID)

	// Verify L1 Object Cache was self-healed
	healed, errCache := object_cache_service.GetPOIFromObjectCache(ctx, 3001)
	require.NoError(t, errCache)
	assert.Equal(t, "Hôtel de Ville", healed.Title)

	// Case B: Cold GEO Cache -> falls back to FuncGetPOIsInBBox and self-heals both L1 Object Cache and GEO Index
	_ = redis.FlushDB(ctx)
	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		assert.Contains(t, query, "content.func_get_pois_in_bbox")
		return test.NewMockRows(cols, [][]driver.Value{
			{
				int64(4001), int64(3), "Sacré-Cœur", "Basilique de Montmartre", 48.8867, 2.3431, "35 Rue du Chevalier de la Barre",
				nil, nil, 4.8, int64(520), "{}", nil, "https://maps.apple.com/?place-id=sacrecoeur", int64(0), now, now,
			},
		}), nil
	}, nil)

	outCold, errCold := map_service.GetPOIsInBBoxService(ctx, 100, map_models.GetPoisInBBoxInput{
		SWLat: 48.87,
		SWLng: 2.32,
		NELat: 48.90,
		NELng: 2.36,
	})
	require.NoError(t, errCold)
	require.Len(t, outCold.POIs, 1)
	assert.Equal(t, int64(4001), outCold.POIs[0].ID)

	// Verify GEO index was self-healed too
	geoIDs, errGeo := object_cache_service.SearchPOIIDsInBBox(ctx, 48.87, 2.32, 48.90, 2.36)
	require.NoError(t, errGeo)
	assert.Contains(t, geoIDs, int64(4001))
}

func TestGetPOIsInBBoxService_FiltersSoftDeletedPOIs(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()
	now := domain.NowMillis()

	// Seed active POI + stale soft-deleted POI in L1
	require.NoError(t, object_cache_service.SetPOIsBatchInObjectCache(ctx, []map_models.POIPayload{
		{
			ID:         5101,
			Type:       1,
			Title:      "POI Actif",
			Latitude:   48.8566,
			Longitude:  2.3522,
			Visibility: variables.POIVisibilityPublic,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		{
			ID:         5102,
			Type:       1,
			Title:      "POI Soft-Deleted",
			Latitude:   48.8567,
			Longitude:  2.3523,
			Visibility: variables.POIVisibilitySoftDeleted,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}))

	out, err := map_service.GetPOIsInBBoxService(ctx, 100, map_models.GetPoisInBBoxInput{
		SWLat: 48.84,
		SWLng: 2.33,
		NELat: 48.87,
		NELng: 2.37,
	})
	require.NoError(t, err)
	require.Len(t, out.POIs, 1)
	assert.Equal(t, int64(5101), out.POIs[0].ID)
}

func TestGetPOIsInBBoxService_InvalidBBox(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	_, err := map_service.GetPOIsInBBoxService(ctx, 1, map_models.GetPoisInBBoxInput{
		SWLat: 50.0,
		SWLng: 2.0,
		NELat: 48.0, // SWLat > NELat
		NELng: 3.0,
	})
	require.Error(t, err)
}
