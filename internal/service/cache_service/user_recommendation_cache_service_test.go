package cache_service_test

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestUserRecommendationCacheService_LanguageIndex(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	// Add users to language 0 and language 1
	require.NoError(t, cache_service.AddUserToLanguageIndex(ctx, 101, 0))
	require.NoError(t, cache_service.AddUserToLanguageIndex(ctx, 102, 0))
	require.NoError(t, cache_service.AddUserToLanguageIndex(ctx, 103, 0))
	require.NoError(t, cache_service.AddUserToLanguageIndex(ctx, 102, 1))
	require.NoError(t, cache_service.AddUserToLanguageIndex(ctx, 103, 1))

	// Single language query
	lang0Users, err := cache_service.GetCandidatesByLanguages(ctx, 0)
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{101, 102, 103}, lang0Users)

	// Multi-language intersection via SINTER
	bothLangUsers, err := cache_service.GetCandidatesByLanguages(ctx, 0, 1)
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{102, 103}, bothLangUsers)

	// Switch language for user 101 from 0 to 1
	require.NoError(t, cache_service.SwitchUserLanguageIndex(ctx, 101, 0, 1))
	lang0After, err := cache_service.GetCandidatesByLanguages(ctx, 0)
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{102, 103}, lang0After)

	lang1After, err := cache_service.GetCandidatesByLanguages(ctx, 1)
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{101, 102, 103}, lang1After)

	// Edge cases: empty langs or negative userID
	emptyCandidates, err := cache_service.GetCandidatesByLanguages(ctx)
	require.NoError(t, err)
	assert.Empty(t, emptyCandidates)
	require.NoError(t, cache_service.AddUserToLanguageIndex(ctx, 0, 0))
}

func TestUserRecommendationCacheService_CuckooSkipsAndRebuild(t *testing.T) {
	_ = test.SetupTestRedis(t)
	defer test.ResetCollections()()
	ctx := context.Background()

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("rebuild_from_mongodb_on_missing_cuckoo_filter_and_exclude_candidates", func(mt *mtest.T) {
		_ = test.SetupTestRedis(mt.T)
		mongo.UserRecoSkips = test.NewMockCollection(mt, variables.MongoCollUserRecoSkips, nil)

		// Mock MongoDB returning skipped users 202 and 204
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "test.user_reco_skips", mtest.FirstBatch,
			bson.D{{Key: "skipped_user_id", Value: int64(202)}},
			bson.D{{Key: "skipped_user_id", Value: int64(204)}},
		))

		candidates := []int64{201, 202, 203, 204, 205}
		filtered, err := cache_service.FilterOutSkippedCandidates(ctx, 42, candidates)
		require.NoError(mt, err)
		assert.Equal(mt, []int64{201, 203, 205}, filtered)

		// Add a new skip synchronously and verify immediate exclusion
		require.NoError(mt, cache_service.AddUserRecoSkipToCuckoo(ctx, 42, 203))
		filteredAfter, err := cache_service.FilterOutSkippedCandidates(ctx, 42, candidates)
		require.NoError(mt, err)
		assert.Equal(mt, []int64{201, 205}, filteredAfter)
	})
}

func TestUserRecommendationCacheService_ComputeLock(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	hasLock, err := cache_service.HasUserRecoComputeLock(ctx, 77)
	require.NoError(t, err)
	assert.False(t, hasLock)

	// First acquisition succeeds
	acquired, err := cache_service.AcquireUserRecoComputeLock(ctx, 77)
	require.NoError(t, err)
	assert.True(t, acquired)

	hasLock, err = cache_service.HasUserRecoComputeLock(ctx, 77)
	require.NoError(t, err)
	assert.True(t, hasLock)

	// Second acquisition while locked returns false
	acquiredAgain, err := cache_service.AcquireUserRecoComputeLock(ctx, 77)
	require.NoError(t, err)
	assert.False(t, acquiredAgain)
}

func TestUserRecommendationCacheService_PublishAndGetRecommendations(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()
	userID := int64(500)

	// Cache miss before publication
	ids, hit, err := cache_service.GetUserRecommendationIDs(ctx, userID, variables.UserRecoSortGlobal, 10)
	require.NoError(t, err)
	assert.False(t, hit)
	assert.Empty(t, ids)

	// Publish all 4 lists
	geo := []cache_service.UserRecoScoredEntry{{UserID: 1, Score: 0.95}, {UserID: 2, Score: 0.80}}
	aff := []cache_service.UserRecoScoredEntry{{UserID: 3, Score: 4.20}, {UserID: 4, Score: 2.10}}
	taste := []cache_service.UserRecoScoredEntry{{UserID: 5, Score: 0.88}, {UserID: 6, Score: 0.77}}
	global := []cache_service.UserRecoScoredEntry{
		{UserID: 1, Score: 150},
		{UserID: 3, Score: 149},
		{UserID: 5, Score: 148},
		{UserID: 2, Score: 147},
	}

	require.NoError(t, cache_service.PublishUserRecommendationLists(ctx, userID, geo, aff, taste, global))

	// Verify exact rank order on each sort mode
	globalIDs, hit, err := cache_service.GetUserRecommendationIDs(ctx, userID, variables.UserRecoSortGlobal, 10)
	require.NoError(t, err)
	assert.True(t, hit)
	assert.Equal(t, []int64{1, 3, 5, 2}, globalIDs)

	geoIDs, hit, err := cache_service.GetUserRecommendationIDs(ctx, userID, variables.UserRecoSortGeo, 10)
	require.NoError(t, err)
	assert.True(t, hit)
	assert.Equal(t, []int64{1, 2}, geoIDs)

	affIDs, hit, err := cache_service.GetUserRecommendationIDs(ctx, userID, variables.UserRecoSortAff, 10)
	require.NoError(t, err)
	assert.True(t, hit)
	assert.Equal(t, []int64{3, 4}, affIDs)

	tasteIDs, hit, err := cache_service.GetUserRecommendationIDs(ctx, userID, variables.UserRecoSortTaste, 10)
	require.NoError(t, err)
	assert.True(t, hit)
	assert.Equal(t, []int64{5, 6}, tasteIDs)

	// Skipping user 3 removes them immediately from the L1 lists
	mongo.UserRecoSkips = nil
	require.NoError(t, cache_service.AddUserRecoSkipToCuckoo(ctx, userID, 3))

	globalAfterSkip, hit, err := cache_service.GetUserRecommendationIDs(ctx, userID, variables.UserRecoSortGlobal, 10)
	require.NoError(t, err)
	assert.True(t, hit)
	assert.Equal(t, []int64{1, 5, 2}, globalAfterSkip)

	// Invalid sort mode returns error
	_, _, errInvalid := cache_service.GetUserRecommendationIDs(ctx, userID, "invalid_sort", 10)
	assert.Error(t, errInvalid)

	// Empty list publication still registers cache hit (empty marker)
	require.NoError(t, cache_service.PublishUserRecommendationLists(ctx, 999, nil, nil, nil, nil))
	emptyIDs, hitEmpty, err := cache_service.GetUserRecommendationIDs(ctx, 999, variables.UserRecoSortGlobal, 10)
	require.NoError(t, err)
	assert.True(t, hitEmpty)
	assert.Empty(t, emptyIDs)
}
