package algorithm_service

import (
	"context"
	"math/rand"
	"strconv"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlgorithmService_LanguageAwareClerkCashierDistributorAndSerendipity(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()
	now := domain.NowTime().UTC()
	dateStr := now.Format("20060102")
	hourStr := now.Format("2006010215")

	// 1. Seed language-partitioned MOST Cache ZSETs for French (LangFR=0) and English (LangEN=1)
	for i := 1; i <= 15; i++ {
		frID := strconv.Itoa(1000 + i)
		enID := strconv.Itoa(2000 + i)
		require.NoError(t, redis.TrendGlobalDaily.ZAdd(ctx, variables.TrendGlobalDailySubKey(variables.LangFR, dateStr), float64(100-i), frID))
		require.NoError(t, redis.TrendGlobalHourly.ZAdd(ctx, variables.TrendGlobalHourlySubKey(variables.LangFR, hourStr), float64(100-i), frID))
		require.NoError(t, redis.TrendTagDaily.ZAdd(ctx, variables.TrendTagDailySubKey(variables.LangFR, "golang", dateStr), float64(90-i), frID))

		require.NoError(t, redis.TrendGlobalDaily.ZAdd(ctx, variables.TrendGlobalDailySubKey(variables.LangEN, dateStr), float64(100-i), enID))
		require.NoError(t, redis.TrendGlobalHourly.ZAdd(ctx, variables.TrendGlobalHourlySubKey(variables.LangEN, hourStr), float64(100-i), enID))

		vec := make([]float32, variables.VectorDimTotal)
		vec[0] = 0.5
		vec[variables.VectorOffSoc] = 0.5
		StoreContentVector(ctx, post_models.PostPayload{
			ID:            int64(1000 + i),
			UserID:        10,
			Vector:        vec,
			PriorityLevel: 1,
			Lang:          variables.LangFR,
		})
		StoreContentVector(ctx, post_models.PostPayload{
			ID:            int64(2000 + i),
			UserID:        20,
			Vector:        vec,
			PriorityLevel: 1,
			Lang:          variables.LangEN,
		})
	}

	// Telemetry profile with tags
	userID := int64(42)
	userVec := make([]float32, variables.VectorDimTotal)
	userVec[0] = 0.5
	userVec[variables.VectorOffSoc] = 0.5
	require.NoError(t, cache_service.SetTelemetryProfile(ctx, userID, lite_models.TelemetryProfileLite{
		Vector:          userVec,
		TopTags:         []string{"golang"},
		ConfidenceScore: 0.5,
		TimestampMs:     now.UnixMilli(),
	}))

	// 2. Test collectCandidates for LangFR vs LangEN isolation
	quotas := Quotas{
		MaxCandidates: 10,
		SocialRatio:   0.2,
		TagRatio:      0.5,
		GlobalRatio:   0.3,
	}
	frBaskets, err := collectCandidates(ctx, userID, [3]int64{11, 22, 33}, quotas, variables.LangFR)
	require.NoError(t, err)
	require.NotEmpty(t, frBaskets.FeedA.Items)
	for _, it := range frBaskets.FeedA.Items {
		assert.True(t, it.PostID >= 1001 && it.PostID <= 1015, "French basket must only draw from French MOST Cache store, got %d", it.PostID)
	}

	enBaskets, err := collectCandidates(ctx, userID, [3]int64{11, 22, 33}, quotas, variables.LangEN)
	require.NoError(t, err)
	require.NotEmpty(t, enBaskets.FeedA.Items)
	for _, it := range enBaskets.FeedA.Items {
		assert.True(t, it.PostID >= 2001 && it.PostID <= 2015, "English basket must only draw from English MOST Cache store, got %d", it.PostID)
	}

	// 3. Test Cold Start Fallback Chain (LangIT=3 has no posts -> falls back to LangFR=0)
	itBaskets, err := collectCandidates(ctx, userID, [3]int64{44, 55, 66}, quotas, variables.LangIT)
	require.NoError(t, err)
	require.NotEmpty(t, itBaskets.FeedA.Items, "Cold-start language must deterministically fall back via GetLanguageFallbackChain")

	// 4. Test HandlePullToRefresh populates all 3 rotating buffers (A, B, C) and marks seen after Cuckoo reset
	refreshOpts := RefreshOptions{
		UserID:        userID,
		LastSeenIndex: 0,
		FetchCount:    5,
		Quotas:        quotas,
		PersonalOpts: PersonalizedFeedOptions{
			UserID:         userID,
			UserVec:        userVec,
			UserConfidence: 0.5,
			Date:           now,
			Limit:          10,
			Lang:           variables.LangFR,
		},
		IsForce: true,
	}

	page1, err := HandlePullToRefresh(ctx, refreshOpts)
	require.NoError(t, err)
	require.NotEmpty(t, page1)

	state, err := GetUserFeedState(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, "A", state.ActiveFeed)
	assert.NotEmpty(t, state.Feeds["A"].PostIDs, "Buffer A must be populated")
	assert.NotEmpty(t, state.Feeds["B"].PostIDs, "Buffer B must be pre-scored during full regeneration")
	assert.NotEmpty(t, state.Feeds["C"].PostIDs, "Buffer C must be pre-scored during full regeneration")

	// 5. Test injectSerendipity never produces duplicate post IDs
	feed := []postCandidate{
		{PostID: 101},
		{PostID: 102},
		{PostID: 103},
		{PostID: 104},
	}
	pool := []int64{101, 102, 103, 104, 201, 202}
	rng := rand.New(rand.NewSource(99))
	resFeed := injectSerendipity(feed, pool, rng, 0)
	seen := make(map[int64]bool)
	for _, c := range resFeed {
		assert.False(t, seen[c.PostID], "duplicate post ID %d after injectSerendipity", c.PostID)
		seen[c.PostID] = true
	}

	// 6. Test computePersonalizedScore single bounded priority multiplication
	scoreIndexed := computePersonalizedScore(50.0, userVec, userVec, 10, nil, 4)
	scoreIndexedNoPrio := computePersonalizedScore(50.0, userVec, userVec, 10, nil, 0)
	assert.InDelta(t, scoreIndexedNoPrio, scoreIndexed, 1e-6, "when trendScore != 1.0, computePersonalizedScore must not double-multiply priority")

	scoreUnindexedPrio := computePersonalizedScore(1.0, userVec, userVec, 10, nil, 4)
	scoreUnindexedNoPrio := computePersonalizedScore(1.0, userVec, userVec, 10, nil, 0)
	assert.InDelta(t, scoreUnindexedNoPrio*2.0, scoreUnindexedPrio, 1e-6, "when trendScore == 1.0, bounded priority [1.0, 2.0] applies")
}
