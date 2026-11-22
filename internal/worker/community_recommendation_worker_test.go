package worker

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunCommunityRecommendationCronCycle_FullScoringAndAtomicReplace(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()

	// Seed stale entry in "de" (which has 0 active communities) to verify stale ZSET cleanup
	require.NoError(t, cache_service.UpsertRankedCommunityScore(ctx, "de", 9999, 50.0))

	// Seed UsersLite trust scores (user 1004 intentionally absent -> fallback 0.50)
	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(1001), lite_models.UserLiteRequest{ID: 1001, Username: "owner_high", TrustScore: 0.95}))
	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(1002), lite_models.UserLiteRequest{ID: 1002, Username: "admin_high", TrustScore: 0.90}))
	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(1003), lite_models.UserLiteRequest{ID: 1003, Username: "owner_low", TrustScore: 0.20}))

	// Seed 7-day telemetry for community 101 (fr, high activity + partner + free_chat)
	now := domain.NowTime().UTC()
	require.NoError(t, cache_service.RecordCommunityMessageActivityAt(ctx, 101, 1001, now))
	require.NoError(t, cache_service.RecordCommunityMessageActivityAt(ctx, 101, 1002, now))
	require.NoError(t, cache_service.RecordCommunityMessageActivityAt(ctx, 101, 1004, now.Add(-24*time.Hour)))
	require.NoError(t, cache_service.RecordCommunityMemberJoinAt(ctx, 101, now))
	require.NoError(t, cache_service.RecordCommunityMemberJoinAt(ctx, 101, now))

	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		switch {
		case strings.Contains(query, "func_load_active_communities_for_reco"):
			return test.NewMockRows(
				[]string{"id", "title", "description", "avatar_id", "lang", "is_partner", "pin_slot", "settings", "created_at", "member_count"},
				[][]driver.Value{
					{int64(101), "FR Active", "Desc", int64(0), "fr", true, nil, `{"write_permission":0}`, domain.TimeToMillis(now.Add(-2 * 24 * time.Hour)), int64(50)},
					{int64(102), "FR Announce", "Desc", int64(0), "fr", false, nil, `{"write_permission":1}`, domain.TimeToMillis(now.Add(-60 * 24 * time.Hour)), int64(10)},
					{int64(201), "EN Sub", "Desc", int64(0), "en", false, nil, `{"write_permission":2}`, domain.TimeToMillis(now.Add(-5 * 24 * time.Hour)), int64(25)},
					{int64(999), "Bad Lang", "Desc", int64(0), "zz", false, nil, `{"write_permission":0}`, domain.TimeToMillis(now), int64(5)},
				},
			), nil
		case strings.Contains(query, "func_load_community_members_for_trust"):
			return test.NewMockRows(
				[]string{"conversation_id", "user_id", "role"},
				[][]driver.Value{
					{int64(101), int64(1001), int64(variables.MemberRoleOwner)},
					{int64(101), int64(1002), int64(variables.MemberRoleAdmin)},
					{int64(101), int64(1004), int64(variables.MemberRoleNormal)}, // missing in UsersLite -> 0.50 fallback
					{int64(102), int64(1003), int64(variables.MemberRoleOwner)},
					{int64(201), int64(1001), int64(variables.MemberRoleOwner)},
				},
			), nil
		default:
			return test.NewMockRows([]string{"id"}, nil), nil
		}
	}, nil)

	stats, err := RunCommunityRecommendationCronCycle(ctx)
	require.NoError(t, err)
	assert.False(t, stats.SkippedLocked)
	assert.Equal(t, 0, stats.ErrorsCount)
	assert.Equal(t, 3, stats.CommunitiesProcessed)
	assert.GreaterOrEqual(t, stats.LanguagesProcessed, 2)

	// Verify FR ranking: 101 must outrank 102
	frRanked, err := cache_service.GetRankedCommunitiesByLang(ctx, "fr")
	require.NoError(t, err)
	require.Len(t, frRanked, 2)
	assert.Equal(t, int64(101), frRanked[0].ConversationID)
	assert.Equal(t, int64(102), frRanked[1].ConversationID)
	assert.Greater(t, frRanked[0].Score, frRanked[1].Score)

	// Verify EN ranking
	enRanked, err := cache_service.GetRankedCommunitiesByLang(ctx, "en")
	require.NoError(t, err)
	require.Len(t, enRanked, 1)
	assert.Equal(t, int64(201), enRanked[0].ConversationID)
	assert.Greater(t, enRanked[0].Score, 0.0)

	// Verify stale DE ZSET was cleared
	deRanked, err := cache_service.GetRankedCommunitiesByLang(ctx, "de")
	require.NoError(t, err)
	assert.Empty(t, deRanked)
}

func TestRunCommunityRecommendationCronCycle_SkipsWhenLocked(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()

	// Pre-acquire distributed cron lock
	token, acquired, err := redis.AcquireCronLock(ctx, variables.CommunityRecoCronLockKey, time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	defer func() { _ = redis.ReleaseCronLock(ctx, variables.CommunityRecoCronLockKey, token) }()

	pgCalled := false
	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		pgCalled = true
		return test.NewMockRows([]string{"id"}, nil), nil
	}, nil)

	stats, err := RunCommunityRecommendationCronCycle(ctx)
	require.NoError(t, err)
	assert.True(t, stats.SkippedLocked)
	assert.False(t, pgCalled, "Postgres must not be queried when cron lock is already held")
}

func TestRunCommunityRecommendationCronCycle_IsolatesPerLanguageErrors(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()
	now := domain.NowTime().UTC()

	// Pre-seed FR with a known good ranking that must survive when FR fails
	require.NoError(t, cache_service.UpsertRankedCommunityScore(ctx, "fr", 101, 77.7))

	memberCall := 0
	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		switch {
		case strings.Contains(query, "func_load_active_communities_for_reco"):
			return test.NewMockRows(
				[]string{"id", "title", "description", "avatar_id", "lang", "is_partner", "pin_slot", "settings", "created_at", "member_count"},
				[][]driver.Value{
					{int64(201), "EN Comm", "Desc", int64(0), "en", false, nil, `{"write_permission":0}`, domain.TimeToMillis(now), int64(15)},
					{int64(101), "FR Comm", "Desc", int64(0), "fr", false, nil, `{"write_permission":0}`, domain.TimeToMillis(now), int64(15)},
				},
			), nil
		case strings.Contains(query, "func_load_community_members_for_trust"):
			memberCall++
			// SupportedLanguageISOs sorted: "en" comes before "fr"
			if memberCall == 2 {
				return nil, errors.New("simulated pg error on fr members")
			}
			return test.NewMockRows(
				[]string{"conversation_id", "user_id", "role"},
				[][]driver.Value{{int64(201), int64(1001), int64(variables.MemberRoleOwner)}},
			), nil
		default:
			return test.NewMockRows([]string{"id"}, nil), nil
		}
	}, nil)

	stats, err := RunCommunityRecommendationCronCycle(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.ErrorsCount)
	assert.Equal(t, 1, stats.CommunitiesProcessed)

	// EN succeeded
	enRanked, err := cache_service.GetRankedCommunitiesByLang(ctx, "en")
	require.NoError(t, err)
	require.Len(t, enRanked, 1)
	assert.Equal(t, int64(201), enRanked[0].ConversationID)

	// FR preserved previous last-known-good score 77.7
	scoreFR, foundFR, err := cache_service.GetRankedCommunityScore(ctx, "fr", 101)
	require.NoError(t, err)
	assert.True(t, foundFR)
	assert.InDelta(t, 77.7, scoreFR, 1e-6)
}
