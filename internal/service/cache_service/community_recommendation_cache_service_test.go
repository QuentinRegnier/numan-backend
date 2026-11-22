package cache_service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommunityRecommendationCache_TelemetryAnd7DayReads(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()

	fixedNow := time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC)

	// Record messages on day 0 (2026-11-21)
	require.NoError(t, RecordCommunityMessageActivityAt(ctx, 1001, 10, fixedNow))
	require.NoError(t, RecordCommunityMessageActivityAt(ctx, 1001, 10, fixedNow)) // same author
	require.NoError(t, RecordCommunityMessageActivityAt(ctx, 1001, 20, fixedNow))
	require.NoError(t, RecordCommunityMessageActivityAt(ctx, 1002, 30, fixedNow))

	// Record growth on day 0
	require.NoError(t, RecordCommunityMemberJoinAt(ctx, 1001, fixedNow))
	require.NoError(t, RecordCommunityMemberJoinAt(ctx, 1001, fixedNow))

	// Record messages & growth on day -2 (2026-11-19)
	dayMinus2 := fixedNow.AddDate(0, 0, -2)
	require.NoError(t, RecordCommunityMessageActivityAt(ctx, 1001, 20, dayMinus2)) // overlapping author across days
	require.NoError(t, RecordCommunityMessageActivityAt(ctx, 1001, 40, dayMinus2)) // new author
	require.NoError(t, RecordCommunityMemberJoinAt(ctx, 1001, dayMinus2))

	// Record messages outside 7-day window (day -10 -> ignored in 7-day read)
	dayMinus10 := fixedNow.AddDate(0, 0, -10)
	require.NoError(t, RecordCommunityMessageActivityAt(ctx, 1001, 999, dayMinus10))
	require.NoError(t, RecordCommunityMemberJoinAt(ctx, 1001, dayMinus10))

	dates := CommunityRecoTrailingDates(fixedNow, 7)
	require.Len(t, dates, 7)
	assert.Equal(t, "2026-11-21", dates[0])
	assert.Equal(t, "2026-11-15", dates[6])

	msgCounts, err := GetCommunity7DayMessageCounts(ctx, []int64{1001, 1002, 1003}, dates)
	require.NoError(t, err)
	assert.Equal(t, int64(5), msgCounts[1001]) // 3 on day 0 + 2 on day -2 (day -10 excluded)
	assert.Equal(t, int64(1), msgCounts[1002])
	assert.Equal(t, int64(0), msgCounts[1003])

	growthCounts, err := GetCommunity7DayGrowthCounts(ctx, []int64{1001, 1002}, dates)
	require.NoError(t, err)
	assert.Equal(t, int64(3), growthCounts[1001]) // 2 on day 0 + 1 on day -2
	assert.Equal(t, int64(0), growthCounts[1002])

	uniqueAuthors, err := GetCommunity7DayUniqueAuthors(ctx, []int64{1001, 1002, 1003}, dates)
	require.NoError(t, err)
	assert.Equal(t, int64(3), uniqueAuthors[1001]) // {10, 20, 40} deduplicated across days
	assert.Equal(t, int64(1), uniqueAuthors[1002]) // {30}
	assert.Equal(t, int64(0), uniqueAuthors[1003])
}

func TestCommunityRecommendationCache_UserPublicCommsAndFriendsPipeline(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()

	// Idempotent add
	added, err := AddUserPublicCommunity(ctx, 10, 500)
	require.NoError(t, err)
	assert.True(t, added)

	addedDup, err := AddUserPublicCommunity(ctx, 10, 500)
	require.NoError(t, err)
	assert.False(t, addedDup)

	// Batch seeding
	require.NoError(t, AddUserPublicCommunitiesBatch(ctx, map[int64][]int64{
		10: {501},
		20: {500, 502},
		30: {500},
	}))

	// Direct friends in both in:2 and out:2 with overlap and cap
	require.NoError(t, redis.SpeedRelationsIndex.ZAdd(ctx, fmt.Sprintf("in:%d:%d", variables.RelationStateFriend, 1), 100, "10"))
	require.NoError(t, redis.SpeedRelationsIndex.ZAdd(ctx, fmt.Sprintf("in:%d:%d", variables.RelationStateFriend, 1), 90, "20"))
	require.NoError(t, redis.SpeedRelationsIndex.ZAdd(ctx, fmt.Sprintf("out:%d:%d", variables.RelationStateFriend, 1), 80, "20")) // dup
	require.NoError(t, redis.SpeedRelationsIndex.ZAdd(ctx, fmt.Sprintf("out:%d:%d", variables.RelationStateFriend, 1), 70, "30"))

	friends, err := GetDirectFriendIDsCapped(ctx, 1, 2)
	require.NoError(t, err)
	assert.Equal(t, []int64{10, 20}, friends) // capped at 2

	allFriends, err := GetDirectFriendIDsCapped(ctx, 1, 50)
	require.NoError(t, err)
	assert.Equal(t, []int64{10, 20, 30}, allFriends)

	friendCommCounts, err := GetFriendsPublicCommunities(ctx, allFriends)
	require.NoError(t, err)
	assert.Equal(t, 3, friendCommCounts[500]) // friends 10, 20, 30
	assert.Equal(t, 1, friendCommCounts[501]) // friend 10
	assert.Equal(t, 1, friendCommCounts[502]) // friend 20

	// Remove one membership
	require.NoError(t, RemoveUserPublicCommunity(ctx, 30, 500))
	friendCommCountsAfter, err := GetFriendsPublicCommunities(ctx, allFriends)
	require.NoError(t, err)
	assert.Equal(t, 2, friendCommCountsAfter[500])
}

func TestCommunityRecommendationCache_RankedPinsAndHydrationBatches(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()

	// Ranked ZSET replace & upsert & remove
	require.NoError(t, ReplaceRankedCommunitiesByLang(ctx, "fr", []RankedCommunityEntry{
		{ConversationID: 100, Score: 4.5},
		{ConversationID: 200, Score: 8.0},
	}))
	ranked, err := GetRankedCommunitiesByLang(ctx, "fr")
	require.NoError(t, err)
	require.Len(t, ranked, 2)
	assert.Equal(t, int64(200), ranked[0].ConversationID)
	assert.Equal(t, int64(100), ranked[1].ConversationID)

	score, ok, err := GetRankedCommunityScore(ctx, "fr", 200)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 8.0, score)

	_, okMissing, err := GetRankedCommunityScore(ctx, "fr", 999)
	require.NoError(t, err)
	assert.False(t, okMissing)

	require.NoError(t, UpsertRankedCommunityScore(ctx, "fr", 300, 10.0))
	require.NoError(t, RemoveRankedCommunity(ctx, "fr", 100))
	rankedAfter, err := GetRankedCommunitiesByLang(ctx, "fr")
	require.NoError(t, err)
	require.Len(t, rankedAfter, 2)
	assert.Equal(t, int64(300), rankedAfter[0].ConversationID)

	// Pins HASH operations
	require.NoError(t, SetCommunityPinInCache(ctx, "fr", 1, 200))
	require.NoError(t, SetCommunityPinInCache(ctx, "fr", 4, 300))
	pins, err := GetCommunityPinsByLang(ctx, "fr")
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{1: 200, 4: 300}, pins)

	require.NoError(t, RemoveCommunityPinByConversationID(ctx, "fr", 300))
	pinsAfter, err := GetCommunityPinsByLang(ctx, "fr")
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{1: 200}, pinsAfter)

	// Hydration batches: ConvMeta, SpeedCommunity / ConvParticipants fallback, UsersLite
	require.NoError(t, redis.ConvMeta.SetObject(ctx, int64(200), lite_models.ConvLiteRequest{
		ID:        200,
		Type:      variables.ConversationTypeCommunityPub,
		Title:     "Go FR",
		Lang:      "fr",
		IsPartner: true,
		PinSlot:   1,
		Mode:      "subreddit",
	}))
	metas, missing, err := GetConvMetaBatch(ctx, []int64{200, 999})
	require.NoError(t, err)
	assert.Contains(t, metas, int64(200))
	assert.Equal(t, []int64{999}, missing)
	assert.Equal(t, "Go FR", metas[200].Title)
	assert.True(t, metas[200].IsPartner)

	require.NoError(t, redis.SpeedCommunity.SetObject(ctx, int64(200), lite_models.CommunityLiteRequest{
		ID:          200,
		Name:        "Go FR",
		MemberCount: 42,
	}))
	require.NoError(t, redis.ConvParticipants.SAdd(ctx, int64(300), 1, 2, 3))
	memberCounts, err := GetCommunityMemberCountsBatch(ctx, []int64{200, 300})
	require.NoError(t, err)
	assert.Equal(t, 42, memberCounts[200])
	assert.Equal(t, 3, memberCounts[300])

	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(10), lite_models.UserLiteRequest{
		ID:         10,
		Username:   "alice",
		TrustScore: 0.85,
	}))
	users, err := GetUsersLiteBatch(ctx, []int64{10, 999})
	require.NoError(t, err)
	assert.InDelta(t, 0.85, users[10].TrustScore, 1e-9)
	assert.NotContains(t, users, int64(999))
}
