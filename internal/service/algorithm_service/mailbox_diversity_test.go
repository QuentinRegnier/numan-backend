package algorithm_service

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedTrendsForMailboxTest(t *testing.T, ctx context.Context, userID int64, count int) {
	t.Helper()
	now := domain.NowTime().UTC()
	dateStr := now.Format("20060102")
	hourStr := now.Format("2006010215")

	for i := 1; i <= count; i++ {
		idStr := strconv.Itoa(5000 + i)
		require.NoError(t, redis.TrendGlobalDaily.ZAdd(ctx, variables.TrendGlobalDailySubKey(variables.LangFR, dateStr), float64(500-i), idStr))
		require.NoError(t, redis.TrendGlobalHourly.ZAdd(ctx, variables.TrendGlobalHourlySubKey(variables.LangFR, hourStr), float64(500-i), idStr))
		require.NoError(t, redis.TrendTagDaily.ZAdd(ctx, variables.TrendTagDailySubKey(variables.LangFR, "golang", dateStr), float64(400-i), idStr))
	}

	userVec := make([]float32, variables.VectorDimTotal)
	userVec[0] = 0.5
	require.NoError(t, cache_service.SetTelemetryProfile(ctx, userID, lite_models.TelemetryProfileLite{
		Vector:          userVec,
		TopTags:         []string{"golang"},
		ConfidenceScore: 0.5,
		TimestampMs:     now.UnixMilli(),
	}))
}

func TestMailbox_SocialRatioCapAndSelectiveRemoval(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()
	userID := int64(700)
	seedTrendsForMailboxTest(t, ctx, userID, 40)

	// 15 posts de 15 auteurs distincts dans la Mailbox (du plus ancien 101 au plus récent 115)
	for i := 1; i <= 15; i++ {
		postID := int64(100 + i)
		authorID := int64(900 + i)
		member := fmt.Sprintf("%d:%d", postID, authorID)
		require.NoError(t, redis.FeedsMailbox.ZAdd(ctx, userID, float64(i*1000), member))
	}

	// Quota : MaxCandidates=20, SocialRatio=0.30 -> plafond exact de 6 posts sociaux
	quotas := Quotas{
		MaxCandidates: 20,
		SocialRatio:   0.30,
		TagRatio:      0.50,
		GlobalRatio:   0.20,
	}

	baskets, err := collectCandidates(ctx, userID, [3]int64{1, 2, 3}, quotas, variables.LangFR)
	require.NoError(t, err)

	socialCount := 0
	var socialIDs []int64
	for _, it := range baskets.FeedA.Items {
		if it.Origin == originSocial {
			socialCount++
			socialIDs = append(socialIDs, it.PostID)
		}
	}

	assert.Equal(t, 6, socialCount, "la Mailbox ne doit pas dépasser SocialRatio (30% de 20 = 6)")
	assert.Equal(t, []int64{115, 114, 113, 112, 111, 110}, socialIDs, "les 6 posts les plus récents doivent être pris")
	assert.Equal(t, 20, baskets.FeedA.size(), "le panier doit être rempli à 100% avec les tendances/tags")
	assert.Equal(t, 20, baskets.FeedB.size())
	assert.Equal(t, 20, baskets.FeedC.size())

	// Vérification de la suppression sélective (ZREM) : les 9 autres posts d'amis restent en Mailbox
	remaining, err := redis.FeedsMailbox.ZRevRange(ctx, userID, 0, -1)
	require.NoError(t, err)
	assert.Len(t, remaining, 9, "les posts non consommés d'autres auteurs doivent rester dans la Mailbox")
	assert.Equal(t, "109:909", remaining[0])

	// Une extension suivante (collectSingleBasket) consomme les 6 suivants et en laisse 3
	nextBasket, err := collectSingleBasket(ctx, userID, 42, quotas, variables.LangFR)
	require.NoError(t, err)
	nextSocial := 0
	for _, it := range nextBasket.Items {
		if it.Origin == originSocial {
			nextSocial++
		}
	}
	assert.Equal(t, 6, nextSocial)
	assert.Equal(t, 20, nextBasket.size())

	remainingAfterSecond, err := redis.FeedsMailbox.ZRevRange(ctx, userID, 0, -1)
	require.NoError(t, err)
	assert.Len(t, remainingAfterSecond, 3)
}

func TestMailbox_PerAuthorCapAndExcessPurge(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()
	userID := int64(701)
	seedTrendsForMailboxTest(t, ctx, userID, 30)

	origCap := variables.MailboxMaxPostsPerAuthor
	variables.MailboxMaxPostsPerAuthor = 2
	defer func() { variables.MailboxMaxPostsPerAuthor = origCap }()

	// Auteur spammeur (authorID=55) publie 30 posts, et un second auteur (authorID=66) publie 2 posts
	for i := 1; i <= 30; i++ {
		member := fmt.Sprintf("%d:%d", 2000+i, 55)
		require.NoError(t, redis.FeedsMailbox.ZAdd(ctx, userID, float64(i*1000), member))
	}
	require.NoError(t, redis.FeedsMailbox.ZAdd(ctx, userID, 500, "3001:66"))
	require.NoError(t, redis.FeedsMailbox.ZAdd(ctx, userID, 400, "3002:66"))

	quotas := Quotas{
		MaxCandidates: 20,
		SocialRatio:   0.30, // socialQuota = 6
		TagRatio:      0.50,
		GlobalRatio:   0.20,
	}

	baskets, err := collectCandidates(ctx, userID, [3]int64{10, 20, 30}, quotas, variables.LangFR)
	require.NoError(t, err)

	var socialIDs []int64
	for _, it := range baskets.FeedA.Items {
		if it.Origin == originSocial {
			socialIDs = append(socialIDs, it.PostID)
		}
	}

	// Le spammeur ne place que ses 2 posts les plus récents (2030, 2029) + les 2 posts de l'auteur 66
	assert.Equal(t, []int64{2030, 2029, 3001, 3002}, socialIDs)
	// Le quota social non utilisé (2 places) est redistribué pour remplir le panier à 100%
	assert.Equal(t, 20, baskets.FeedA.size())

	// Les 28 posts excédentaires du spammeur ont été purgés de la Mailbox
	remaining, err := redis.FeedsMailbox.ZRevRange(ctx, userID, 0, -1)
	require.NoError(t, err)
	assert.Empty(t, remaining, "les posts excédentaires du spammeur doivent être purgés de la Mailbox")
}

func TestMailbox_LegacyFormatAndSeenPostsFiltering(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()
	userID := int64(702)
	seedTrendsForMailboxTest(t, ctx, userID, 20)

	// Post 4003 déjà vu par l'utilisateur
	service.MarkAsSeen(ctx, userID, 4003)

	// Mix de format legacy ("postID") et nouveau format ("postID:authorID")
	require.NoError(t, redis.FeedsMailbox.ZAdd(ctx, userID, 3000, "4003:88")) // déjà vu -> purgé sans consommer de quota
	require.NoError(t, redis.FeedsMailbox.ZAdd(ctx, userID, 2000, "4002"))    // format legacy
	require.NoError(t, redis.FeedsMailbox.ZAdd(ctx, userID, 1000, "4001:88")) // non vu

	quotas := Quotas{
		MaxCandidates: 10,
		SocialRatio:   0.30, // socialQuota = 3
		TagRatio:      0.50,
		GlobalRatio:   0.20,
	}

	basket, err := collectSingleBasket(ctx, userID, 99, quotas, variables.LangFR)
	require.NoError(t, err)

	var socialIDs []int64
	for _, it := range basket.Items {
		if it.Origin == originSocial {
			socialIDs = append(socialIDs, it.PostID)
		}
	}
	assert.Equal(t, []int64{4002, 4001}, socialIDs)
	assert.Equal(t, 10, basket.size())

	remaining, err := redis.FeedsMailbox.ZRevRange(ctx, userID, 0, -1)
	require.NoError(t, err)
	assert.Empty(t, remaining)
}

func TestFillBasket_RedistributesUnusedSocialQuotaWhenMailboxEmpty(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()
	userID := int64(703)
	seedTrendsForMailboxTest(t, ctx, userID, 40)

	quotas := Quotas{
		MaxCandidates: 20,
		SocialRatio:   0.30,
		TagRatio:      0.50,
		GlobalRatio:   0.20,
	}

	baskets, err := collectCandidates(ctx, userID, [3]int64{7, 8, 9}, quotas, variables.LangFR)
	require.NoError(t, err)
	assert.Equal(t, 20, baskets.FeedA.size(), "même avec une Mailbox vide, le panier doit atteindre 100% de MaxCandidates")
}

func TestCashier_PerAuthorDiversityCapAndSoftFallback(t *testing.T) {
	origCap := variables.FeedMaxPostsPerAuthor
	variables.FeedMaxPostsPerAuthor = 2
	defer func() { variables.FeedMaxPostsPerAuthor = origCap }()

	vec := make([]float32, variables.VectorDimTotal)
	vec[0] = 1.0

	// 1. Cas nominal : Auteur 10 a 6 posts très bien scorés, Auteurs 20/30/40/50 ont 1 post chacun
	candidates := []postCandidate{
		{PostID: 1, AuthorID: 10, PersonalScore: 100, ContentVec: vec, MatrixIdx: 0},
		{PostID: 2, AuthorID: 10, PersonalScore: 99, ContentVec: vec, MatrixIdx: 1},
		{PostID: 3, AuthorID: 10, PersonalScore: 98, ContentVec: vec, MatrixIdx: 2},
		{PostID: 4, AuthorID: 10, PersonalScore: 97, ContentVec: vec, MatrixIdx: 3},
		{PostID: 5, AuthorID: 10, PersonalScore: 96, ContentVec: vec, MatrixIdx: 4},
		{PostID: 6, AuthorID: 10, PersonalScore: 95, ContentVec: vec, MatrixIdx: 5},
		{PostID: 7, AuthorID: 20, PersonalScore: 50, ContentVec: vec, MatrixIdx: 6},
		{PostID: 8, AuthorID: 30, PersonalScore: 49, ContentVec: vec, MatrixIdx: 7},
		{PostID: 9, AuthorID: 40, PersonalScore: 48, ContentVec: vec, MatrixIdx: 8},
		{PostID: 10, AuthorID: 50, PersonalScore: 47, ContentVec: vec, MatrixIdx: 9},
	}

	simMatrix, dim := buildSimilarityMatrix(candidates)
	selected := runMMR(candidates, simMatrix, dim, variables.TDDLambdaMMR, 6)
	require.Len(t, selected, 6)

	authorCounts := make(map[int64]int)
	for _, c := range selected {
		authorCounts[c.AuthorID]++
	}
	assert.Equal(t, 2, authorCounts[10], "l'auteur 10 ne doit pas dépasser FeedMaxPostsPerAuthor=2 dans le lot MMR")
	assert.Equal(t, 1, authorCounts[20])
	assert.Equal(t, 1, authorCounts[30])
	assert.Equal(t, 1, authorCounts[40])
	assert.Equal(t, 1, authorCounts[50])

	// Vérifie également extractIDsFromCandidates (chemin sans vecteur utilisateur)
	coldIDs := extractIDsFromCandidates(candidates, 6)
	require.Len(t, coldIDs, 6)
	assert.Equal(t, []int64{1, 2, 7, 8, 9, 10}, coldIDs)

	// 2. Cas repli souple (Cold-Start) : un seul auteur dans tout le pool -> ne doit jamais tronquer le feed
	monoAuthor := candidates[:5]
	simMono, dimMono := buildSimilarityMatrix(monoAuthor)
	selectedMono := runMMR(monoAuthor, simMono, dimMono, variables.TDDLambdaMMR, 5)
	assert.Len(t, selectedMono, 5, "le repli souple doit compléter le feed si aucun autre auteur n'est disponible")
	assert.Len(t, extractIDsFromCandidates(monoAuthor, 5), 5)
}
