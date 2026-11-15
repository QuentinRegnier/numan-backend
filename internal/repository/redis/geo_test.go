package redis_test

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeoAddAndGeoSearchBBox(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	require.NotNil(t, repoRedis.POIsGeo)
	assert.Equal(t, repoRedis.POIsGeo.DefaultTTL.Seconds(), float64(0))

	// Add 3 POIs: 2 in Paris, 1 in Marseille
	err := repoRedis.POIsGeo.GeoAddBatch(ctx, "global", []repoRedis.GeoLocationItem{
		{MemberID: 1001, Longitude: 2.294481, Latitude: 48.858370}, // Eiffel Tower
		{MemberID: 1002, Longitude: 2.337644, Latitude: 48.860611}, // Louvre
		{MemberID: 2001, Longitude: 5.369780, Latitude: 43.296482}, // Marseille
	})
	require.NoError(t, err)

	// Search Paris BBox
	ids, err := repoRedis.POIsGeo.GeoSearchBBox(ctx, "global", 48.80, 2.20, 48.90, 2.45, 100)
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{1001, 1002}, ids)

	// Remove 1001 and search again
	require.NoError(t, repoRedis.POIsGeo.GeoRem(ctx, "global", 1001))
	idsAfter, err := repoRedis.POIsGeo.GeoSearchBBox(ctx, "global", 48.80, 2.20, 48.90, 2.45, 100)
	require.NoError(t, err)
	assert.Equal(t, []int64{1002}, idsAfter)
}

func TestGeoSingleAddAndEmptyOperations(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	require.NoError(t, repoRedis.POIsGeo.GeoAddBatch(ctx, "global", nil))
	require.NoError(t, repoRedis.POIsGeo.GeoRem(ctx, "global"))

	require.NoError(t, repoRedis.POIsGeo.GeoAdd(ctx, "global", 2.3522, 48.8566, 3001))
	ids, err := repoRedis.POIsGeo.GeoSearchBBox(ctx, "global", 48.84, 2.34, 48.87, 2.37, 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{3001}, ids)
}
