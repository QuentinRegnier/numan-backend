package community_recommendation_service

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func defaultTestScoringConfig() community_recommendation_models.ScoringConfig {
	return LoadScoringConfig()
}

func TestComputeGlobalScore_ZeroActivityAndMembers(t *testing.T) {
	cfg := defaultTestScoringConfig()
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

	breakdown := ComputeGlobalScoreWithBreakdown(community_recommendation_models.CommunityScoreInput{
		ConversationID:  1,
		MemberCount:     0,
		MsgCount7d:      0,
		UniqueAuthors7d: 0,
		Growth7d:        0,
		Mode:            models.CommunityModeFreeChat,
		CreatedAt:       now.AddDate(0, 0, -30),
		Now:             now,
	}, cfg)

	assert.Equal(t, 0.0, breakdown.GlobalScore)
	assert.InDelta(t, 0.50, breakdown.CommunityTrust, 1e-9)
	assert.InDelta(t, 1.00, breakdown.TrustMultiplier, 1e-9)
	assert.InDelta(t, 1.00, breakdown.FreshnessMultiplier, 1e-9)
}

func TestComputeGlobalScore_AntiSpamSingleAuthorVsOrganic(t *testing.T) {
	cfg := defaultTestScoringConfig()
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	created := now.AddDate(0, 0, -30)

	// 1 author posting 500 messages (spam/bot flood)
	spam := ComputeGlobalScoreWithBreakdown(community_recommendation_models.CommunityScoreInput{
		ConversationID:  1,
		MemberCount:     50,
		MsgCount7d:      500,
		UniqueAuthors7d: 1,
		Growth7d:        5,
		HasOwnerTrust:   true,
		OwnerTrust:      0.50,
		Mode:            models.CommunityModeFreeChat,
		CreatedAt:       created,
		Now:             now,
	}, cfg)

	// 25 unique authors posting 4 messages each (100 messages total)
	organic := ComputeGlobalScoreWithBreakdown(community_recommendation_models.CommunityScoreInput{
		ConversationID:  2,
		MemberCount:     50,
		MsgCount7d:      100,
		UniqueAuthors7d: 25,
		Growth7d:        5,
		HasOwnerTrust:   true,
		OwnerTrust:      0.50,
		Mode:            models.CommunityModeFreeChat,
		CreatedAt:       created,
		Now:             now,
	}, cfg)

	assert.Greater(t, organic.DynamismScore, spam.DynamismScore)
	assert.Greater(t, organic.GlobalScore, spam.GlobalScore)
	assert.InDelta(t, math.Sqrt(math.Log1p(500)*math.Log1p(1)), spam.DynamismScore, 1e-9)
	assert.InDelta(t, math.Sqrt(math.Log1p(100)*math.Log1p(25)), organic.DynamismScore, 1e-9)
}

func TestComputeHierarchicalTrust_WeightRedistributionAndBayesianSmoothing(t *testing.T) {
	cfg := defaultTestScoringConfig()

	t.Run("no delegated admins redistributes weights to owner and members", func(t *testing.T) {
		tComm, mTrust := ComputeHierarchicalTrust(true, 1.0, nil, nil, cfg)
		expectedTComm := (0.45/0.70)*1.0 + (0.25/0.70)*0.50
		assert.InDelta(t, expectedTComm, tComm, 1e-9)
		assert.InDelta(t, 0.5+expectedTComm, mTrust, 1e-9)
	})

	t.Run("with delegated admins uses full three-tier weights", func(t *testing.T) {
		members := make([]float64, 10)
		for i := range members {
			members[i] = 0.9
		}
		tComm, mTrust := ComputeHierarchicalTrust(true, 1.0, []float64{0.8, 0.6}, members, cfg)
		assert.InDelta(t, 0.835, tComm, 1e-9)
		assert.InDelta(t, 1.335, mTrust, 1e-9)
	})

	t.Run("missing owner falls back to priorMean and low trust penalizes", func(t *testing.T) {
		tCommNeutral, mTrustNeutral := ComputeHierarchicalTrust(false, 0, nil, nil, cfg)
		assert.InDelta(t, 0.50, tCommNeutral, 1e-9)
		assert.InDelta(t, 1.00, mTrustNeutral, 1e-9)

		lowMembers := make([]float64, 50)
		for i := range lowMembers {
			lowMembers[i] = 0.0
		}
		tCommLow, mTrustLow := ComputeHierarchicalTrust(true, 0.0, []float64{0.0}, lowMembers, cfg)
		assert.Less(t, tCommLow, 0.05)
		assert.InDelta(t, 0.5+tCommLow, mTrustLow, 1e-9)
		assert.GreaterOrEqual(t, mTrustLow, 0.50)
	})

	t.Run("member sample capped at TrustMemberSampleCap", func(t *testing.T) {
		cfgSmallCap := cfg
		cfgSmallCap.TrustMemberSampleCap = 2
		members := append([]float64{1.0, 1.0}, make([]float64, 100)...)
		tComm, _ := ComputeHierarchicalTrust(true, 0.5, nil, members, cfgSmallCap)
		expectedMember := (10*0.5 + 2.0) / 12.0
		expectedComm := (0.45/0.70)*0.5 + (0.25/0.70)*expectedMember
		assert.InDelta(t, expectedComm, tComm, 1e-9)
	})
}

func TestModeAndPartnerMultipliers(t *testing.T) {
	cfg := defaultTestScoringConfig()
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	baseIn := community_recommendation_models.CommunityScoreInput{
		ConversationID:  10,
		MemberCount:     100,
		MsgCount7d:      50,
		UniqueAuthors7d: 10,
		Growth7d:        10,
		HasOwnerTrust:   true,
		OwnerTrust:      0.50,
		CreatedAt:       now.AddDate(0, 0, -30),
		Now:             now,
	}

	freeChat := baseIn
	freeChat.Mode = models.CommunityModeFreeChat
	sFree := ComputeGlobalScore(freeChat, cfg)

	subreddit := baseIn
	subreddit.Mode = models.CommunityModeSubreddit
	sSub := ComputeGlobalScore(subreddit, cfg)

	announcement := baseIn
	announcement.Mode = models.CommunityModeAnnouncement
	sAnn := ComputeGlobalScore(announcement, cfg)

	partner := freeChat
	partner.IsPartner = true
	sPartner := ComputeGlobalScore(partner, cfg)

	assert.InDelta(t, sFree*(cfg.ModeMultSubreddit/cfg.ModeMultFreeChat), sSub, 1e-9)
	assert.InDelta(t, sFree*(cfg.ModeMultAnnouncement/cfg.ModeMultFreeChat), sAnn, 1e-9)
	assert.Less(t, sAnn, sFree)
	assert.InDelta(t, sFree*cfg.PartnerMultiplier, sPartner, 1e-9)
}

func TestComputeFreshnessMultiplier_DecayCurve(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

	assert.InDelta(t, 1.30, ComputeFreshnessMultiplier(now, now, 0.30, 14), 1e-9)
	assert.InDelta(t, 1.30, ComputeFreshnessMultiplier(now.Add(2*time.Hour), now, 0.30, 14), 1e-9)
	assert.InDelta(t, 1.15, ComputeFreshnessMultiplier(now.AddDate(0, 0, -7), now, 0.30, 14), 1e-9)
	assert.InDelta(t, 1.00, ComputeFreshnessMultiplier(now.AddDate(0, 0, -14), now, 0.30, 14), 1e-9)
	assert.InDelta(t, 1.00, ComputeFreshnessMultiplier(now.AddDate(0, 0, -60), now, 0.30, 14), 1e-9)
	assert.InDelta(t, 1.00, ComputeFreshnessMultiplier(time.Time{}, now, 0.30, 14), 1e-9)
}

func TestApplyFriendBoost_RankingAndDeterministicTieBreak(t *testing.T) {
	candidates := []community_recommendation_models.RankedCandidate{
		{ConversationID: 30, GlobalScore: 10.0},
		{ConversationID: 10, GlobalScore: 10.0},
		{ConversationID: 20, GlobalScore: 9.0},
	}
	friendCounts := map[int64]int{
		20: 5,
		10: 0,
	}

	res := ApplyFriendBoost(candidates, friendCounts, 0.15)
	require.Len(t, res, 3)

	assert.Equal(t, int64(20), res[0].ConversationID)
	assert.Equal(t, 5, res[0].FriendCount)
	assert.InDelta(t, 9.0*(1.0+0.15*math.Log1p(5)), res[0].PersonalizedScore, 1e-9)

	assert.Equal(t, int64(10), res[1].ConversationID)
	assert.Equal(t, int64(30), res[2].ConversationID)
}

func TestInjectPinnedSlots_ExactSlotsNoDuplicatesAndSparseCompaction(t *testing.T) {
	makeOrganic := func(ids ...int64) []community_recommendation_models.PersonalizedCandidate {
		out := make([]community_recommendation_models.PersonalizedCandidate, len(ids))
		for i, id := range ids {
			out[i] = community_recommendation_models.PersonalizedCandidate{
				ConversationID:    id,
				GlobalScore:       float64(100 - i*10),
				PersonalizedScore: float64(100 - i*10),
			}
		}
		return out
	}

	t.Run("exact slot 1 and slot 3 without duplicates", func(t *testing.T) {
		organic := makeOrganic(1, 2, 3, 4, 5)
		pins := map[int]int64{
			1: 5,
			3: 2,
		}
		assembled := InjectPinnedSlots(organic, pins, nil)
		require.Len(t, assembled, 5)

		gotIDs := []int64{assembled[0].ConversationID, assembled[1].ConversationID, assembled[2].ConversationID, assembled[3].ConversationID, assembled[4].ConversationID}
		assert.Equal(t, []int64{5, 1, 2, 3, 4}, gotIDs)

		assert.True(t, assembled[0].IsPinned)
		assert.Equal(t, 1, assembled[0].PinSlot)

		assert.False(t, assembled[1].IsPinned)
		assert.Equal(t, 0, assembled[1].PinSlot)

		assert.True(t, assembled[2].IsPinned)
		assert.Equal(t, 3, assembled[2].PinSlot)
	})

	t.Run("sparse slot 8 with only 3 communities compacts with zero empty holes", func(t *testing.T) {
		organic := makeOrganic(10, 20, 30)
		pins := map[int]int64{8: 10}
		assembled := InjectPinnedSlots(organic, pins, nil)
		require.Len(t, assembled, 3)
		assert.Equal(t, []int64{20, 30, 10}, []int64{assembled[0].ConversationID, assembled[1].ConversationID, assembled[2].ConversationID})
		assert.True(t, assembled[2].IsPinned)
		assert.Equal(t, 8, assembled[2].PinSlot)
	})

	t.Run("pagination across pinned boundaries produces zero duplicates", func(t *testing.T) {
		organic := makeOrganic(1, 2, 3, 4, 5, 6)
		pins := map[int]int64{1: 6, 4: 5}
		assembled := InjectPinnedSlots(organic, pins, nil)
		page1, hasMore1 := PaginateCandidates(assembled, 0, 3)
		assert.True(t, hasMore1)
		require.Len(t, page1, 3)
		assert.Equal(t, []int64{6, 1, 2}, []int64{page1[0].ConversationID, page1[1].ConversationID, page1[2].ConversationID})

		page2, hasMore2 := PaginateCandidates(assembled, 3, 3)
		assert.False(t, hasMore2)
		require.Len(t, page2, 3)
		assert.Equal(t, []int64{5, 3, 4}, []int64{page2[0].ConversationID, page2[1].ConversationID, page2[2].ConversationID})

		page3, hasMore3 := PaginateCandidates(assembled, 6, 3)
		assert.False(t, hasMore3)
		assert.Empty(t, page3)
	})

	t.Run("stale pin missing from both organic and extraPinned is safely ignored", func(t *testing.T) {
		organic := makeOrganic(1, 2)
		pins := map[int]int64{1: 999}
		assembled := InjectPinnedSlots(organic, pins, nil)
		require.Len(t, assembled, 2)
		assert.Equal(t, []int64{1, 2}, []int64{assembled[0].ConversationID, assembled[1].ConversationID})
	})

	t.Run("extraPinned injects active pinned community not yet in organic ZSET", func(t *testing.T) {
		organic := makeOrganic(1, 2)
		pins := map[int]int64{2: 55}
		extra := map[int64]community_recommendation_models.PersonalizedCandidate{
			55: {ConversationID: 55, FriendCount: 2},
		}
		assembled := InjectPinnedSlots(organic, pins, extra)
		require.Len(t, assembled, 3)
		assert.Equal(t, []int64{1, 55, 2}, []int64{assembled[0].ConversationID, assembled[1].ConversationID, assembled[2].ConversationID})
		assert.True(t, assembled[1].IsPinned)
		assert.Equal(t, 2, assembled[1].PinSlot)
		assert.Equal(t, 2, assembled[1].FriendCount)
	})
}

func TestScoring_InvalidAndExtremeConfigValuesSafe(t *testing.T) {
	badCfg := community_recommendation_models.ScoringConfig{
		WeightMembers:      math.NaN(),
		WeightDynamism:     -5.0,
		WeightGrowth:       math.Inf(1),
		AlphaOwner:         -1,
		AlphaAdmin:         0,
		AlphaMember:        0,
		TrustPriorMean:     2.5,
		TrustPriorStrength: -10,
		ModeMultFreeChat:   -1,
		PartnerMultiplier:  math.NaN(),
		FreshnessBoost:     -0.5,
		FreshnessDays:      -3,
		FriendGamma:        math.NaN(),
	}
	s := ComputeGlobalScore(community_recommendation_models.CommunityScoreInput{
		ConversationID:  1,
		MemberCount:     -10,
		MsgCount7d:      -5,
		UniqueAuthors7d: -2,
		Growth7d:        -1,
		HasOwnerTrust:   true,
		OwnerTrust:      math.NaN(),
		Mode:            models.CommunityModeFreeChat,
		IsPartner:       true,
	}, badCfg)
	assert.False(t, math.IsNaN(s))
	assert.False(t, math.IsInf(s, 0))
	assert.GreaterOrEqual(t, s, 0.0)
}

func BenchmarkPersonalizeAndInjectPins(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		candidates := make([]community_recommendation_models.RankedCandidate, n)
		friendCounts := make(map[int64]int, 50)
		for i := 0; i < n; i++ {
			id := int64(10000 + i)
			candidates[i] = community_recommendation_models.RankedCandidate{
				ConversationID: id,
				GlobalScore:    float64(n-i) * 0.1,
			}
			if i%20 == 0 {
				friendCounts[id] = (i % 7) + 1
			}
		}
		pins := map[int]int64{
			1: int64(10000 + n - 1),
			3: int64(10000 + n - 2),
			5: int64(10000 + n - 3),
		}

		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				perso := ApplyFriendBoost(candidates, friendCounts, 0.15)
				assembled := InjectPinnedSlots(perso, pins, nil)
				_, _ = PaginateCandidates(assembled, 0, 20)
			}
		})
	}
}
