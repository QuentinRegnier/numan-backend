package user_recommendation_service_test

import (
	"math"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/user_recommendation_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeCommunityMultiplier_TFIDF(t *testing.T) {
	t.Run("no_shared_communities_returns_1.0", func(t *testing.T) {
		mult := user_recommendation_service.ComputeCommunityMultiplier(10000, nil)
		assert.Equal(t, 1.0, mult)
	})

	t.Run("division_by_zero_and_invalid_counts_guarded", func(t *testing.T) {
		assert.Equal(t, 0.0, user_recommendation_service.ComputeCommunityIDF(0, 100))
		assert.Equal(t, 0.0, user_recommendation_service.ComputeCommunityIDF(1000, 0))
		assert.Equal(t, 0.0, user_recommendation_service.ComputeCommunityIDF(1000, -5))
		// Very large community >= N clamps IDF to 0
		assert.Equal(t, 0.0, user_recommendation_service.ComputeCommunityIDF(1000, 1000))
		assert.Equal(t, 0.0, user_recommendation_service.ComputeCommunityIDF(1000, 5000))
	})

	t.Run("small_community_has_higher_idf_than_large_community", func(t *testing.T) {
		totalUsers := int64(10000)
		idfSmall := user_recommendation_service.ComputeCommunityIDF(totalUsers, 10)   // log(1000) ≈ 6.9077
		idfLarge := user_recommendation_service.ComputeCommunityIDF(totalUsers, 1000) // log(10) ≈ 2.3025

		assert.Greater(t, idfSmall, idfLarge)
		assert.InDelta(t, math.Log(1000.0), idfSmall, 1e-6)
		assert.InDelta(t, math.Log(10.0), idfLarge, 1e-6)

		mult := user_recommendation_service.ComputeCommunityMultiplier(totalUsers, []int64{10, 1000})
		assert.InDelta(t, 1.0+math.Log(1000.0)+math.Log(10.0), mult, 1e-6)
	})
}

func TestComputeTasteAndAffinityScores(t *testing.T) {
	var v1, v2 [user_recommendation_service.VectorDimension]float32
	for i := 0; i < user_recommendation_service.VectorDimension; i++ {
		v1[i] = 0.5
		v2[i] = 0.5
	}
	// DotProduct = 224 * 0.25 = 56.0
	mult := 1.5
	taste := user_recommendation_service.ComputeTasteScore(&v1, &v2, mult)
	assert.InDelta(t, 56.0*1.5, taste, 1e-5)

	assert.Equal(t, 0.0, user_recommendation_service.ComputeTasteScore(nil, &v2, mult))

	// Affinity score: (1.0 + mutualFriends * omega) * CommunityMultiplier
	aff0 := user_recommendation_service.ComputeAffinityScore(0, 0.35, 1.0)
	aff5 := user_recommendation_service.ComputeAffinityScore(5, 0.35, 1.0)
	aff5WithComm := user_recommendation_service.ComputeAffinityScore(5, 0.35, 2.0)

	assert.InDelta(t, 1.0, aff0, 1e-6)
	assert.InDelta(t, 1.0+5*0.35, aff5, 1e-6)
	assert.InDelta(t, (1.0+5*0.35)*2.0, aff5WithComm, 1e-6)
	assert.Greater(t, aff5, aff0)
}

func TestGeolocationScoring_DecayAndPrivacy(t *testing.T) {
	t.Run("distance_decay_monotonicity", func(t *testing.T) {
		d0 := user_recommendation_service.ComputeGeoDecayFromDistanceKm(0, 50.0)
		d10 := user_recommendation_service.ComputeGeoDecayFromDistanceKm(10, 50.0)
		d100 := user_recommendation_service.ComputeGeoDecayFromDistanceKm(100, 50.0)
		d500 := user_recommendation_service.ComputeGeoDecayFromDistanceKm(500, 50.0)

		assert.InDelta(t, 1.0, d0, 1e-6)
		assert.Greater(t, d0, d10)
		assert.Greater(t, d10, d100)
		assert.Greater(t, d100, d500)
		assert.Greater(t, d500, 0.0)
	})

	t.Run("privacy_respected_when_show_location_false_or_missing", func(t *testing.T) {
		factor, ok := user_recommendation_service.ComputeGeoProximityFactor("48.8566,2.3522", false, "48.8566,2.3522", true, 50.0)
		assert.False(t, ok)
		assert.Equal(t, 0.0, factor)

		factor2, ok2 := user_recommendation_service.ComputeGeoProximityFactor("", true, "Paris", true, 50.0)
		assert.False(t, ok2)
		assert.Equal(t, 0.0, factor2)
	})

	t.Run("coordinates_and_city_lookup_haversine", func(t *testing.T) {
		// Paris to Paris -> distance 0 -> factor 1.0
		fSame, ok := user_recommendation_service.ComputeGeoProximityFactor("Paris", true, "48.8566,2.3522", true, 50.0)
		require.True(t, ok)
		assert.InDelta(t, 1.0, fSame, 1e-3)

		// Paris to Lyon (~392 km) < Paris toParis
		fLyon, ok := user_recommendation_service.ComputeGeoProximityFactor("Paris", true, "Lyon", true, 50.0)
		require.True(t, ok)
		assert.Less(t, fLyon, fSame)
		assert.Greater(t, fLyon, 0.0)

		scoreGeo := user_recommendation_service.ComputeGeoScore(fSame, 1.5)
		assert.InDelta(t, 1.5, scoreGeo, 1e-3)
	})
}

func TestBuildCategoryRankingsAndRoundRobinInterleaving(t *testing.T) {
	candidates := []user_recommendation_service.ScoredCandidate{
		{UserID: 10, GeoScore: 0.95, HasGeo: true, AffScore: 1.2, TasteScore: 0.30, HasTaste: true},
		{UserID: 20, GeoScore: 0.40, HasGeo: true, AffScore: 5.0, TasteScore: 0.20, HasTaste: true},
		{UserID: 30, GeoScore: 0.10, HasGeo: true, AffScore: 1.0, TasteScore: 0.99, HasTaste: true},
		{UserID: 40, GeoScore: 0.85, HasGeo: true, AffScore: 4.0, TasteScore: 0.88, HasTaste: true},
	}

	geoList, affList, tasteList := user_recommendation_service.BuildCategoryRankings(candidates, 150)

	// Geo order: 10 (0.95), 40 (0.85), 20 (0.40), 30 (0.10)
	require.Len(t, geoList, 4)
	assert.Equal(t, int64(10), geoList[0].UserID)
	assert.Equal(t, int64(40), geoList[1].UserID)

	// Aff order: 20 (5.0), 40 (4.0), 10 (1.2), 30 (1.0)
	require.Len(t, affList, 4)
	assert.Equal(t, int64(20), affList[0].UserID)
	assert.Equal(t, int64(40), affList[1].UserID)

	// Taste order: 30 (0.99), 40 (0.88), 10 (0.30), 20 (0.20)
	require.Len(t, tasteList, 4)
	assert.Equal(t, int64(30), tasteList[0].UserID)
	assert.Equal(t, int64(40), tasteList[1].UserID)

	// Round-Robin:
	// i=0: Geo#1 (10) -> Aff#1 (20) -> Taste#1 (30)
	// i=1: Geo#2 (40) -> Aff#2 (40 dup) -> Taste#2 (40 dup)
	globalList := user_recommendation_service.InterleaveGlobalRecommendations(geoList, affList, tasteList, 150)
	require.Len(t, globalList, 4)
	assert.Equal(t, []int64{10, 20, 30, 40}, []int64{
		globalList[0].UserID,
		globalList[1].UserID,
		globalList[2].UserID,
		globalList[3].UserID,
	})
	// Scores strictly decreasing so ZREVRANGE preserves round-robin order
	assert.Greater(t, globalList[0].Score, globalList[1].Score)
	assert.Greater(t, globalList[1].Score, globalList[2].Score)
	assert.Greater(t, globalList[2].Score, globalList[3].Score)

	// Verify maxGlobal cap (e.g. 2)
	capped := user_recommendation_service.InterleaveGlobalRecommendations(geoList, affList, tasteList, 2)
	require.Len(t, capped, 2)
	assert.Equal(t, int64(10), capped[0].UserID)
	assert.Equal(t, int64(20), capped[1].UserID)

	// Uneven list lengths handled cleanly
	uneven := user_recommendation_service.InterleaveGlobalRecommendations(
		[]cache_service.UserRecoScoredEntry{{UserID: 1, Score: 10}},
		nil,
		[]cache_service.UserRecoScoredEntry{{UserID: 2, Score: 9}, {UserID: 3, Score: 8}},
		150,
	)
	require.Len(t, uneven, 3)
	assert.Equal(t, []int64{1, 2, 3}, []int64{uneven[0].UserID, uneven[1].UserID, uneven[2].UserID})
}
