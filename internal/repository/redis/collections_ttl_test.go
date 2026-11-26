package redis_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectionsTTLRegistryAndRefreshStandardTTL(t *testing.T) {
	_ = test.SetupTestRedis(t)

	type colSpec struct {
		name           string
		col            *repoRedis.Collection
		expectedTTL    time.Duration
		usesStandardTT bool
	}

	specs := []colSpec{
		// Famille 1 : OBJECT Cache (19)
		{"Users", repoRedis.Users, variables.StandardTTL, true},
		{"UserSettings", repoRedis.UserSettings, variables.StandardTTL, true},
		{"Posts", repoRedis.Posts, variables.StandardTTL, true},
		{"Comments", repoRedis.Comments, variables.StandardTTL, true},
		{"Media", repoRedis.Media, variables.StandardTTL, true},
		{"Messages", repoRedis.Messages, variables.StandardTTL, true},
		{"Conversations", repoRedis.Conversations, variables.StandardTTL, true},
		{"Members", repoRedis.Members, variables.StandardTTL, true},
		{"Relations", repoRedis.Relations, variables.StandardTTL, true},
		{"Saved", repoRedis.Saved, variables.StandardTTL, true},
		{"PostComments", repoRedis.PostComments, variables.StandardTTL, true},
		{"UserTimeline", repoRedis.UserTimeline, variables.StandardTTL, true},
		{"NotificationsObj", repoRedis.NotificationsObj, variables.StandardTTL, true},
		{"MessageReactionCounts", repoRedis.MessageReactionCounts, variables.StandardTTL, true},
		{"MessageUserReactions", repoRedis.MessageUserReactions, variables.StandardTTL, true},
		{"POIReviews", repoRedis.POIReviews, variables.StandardTTL, true},
		{"POIReviewsByPOI", repoRedis.POIReviewsByPOI, variables.StandardTTL, true},
		{"POIReviewUserIndex", repoRedis.POIReviewUserIndex, variables.StandardTTL, true},
		{"POIs", repoRedis.POIs, variables.NoTTL, false},

		// Famille 2 : Auth, Sessions & 2FA (5)
		{"Sessions", repoRedis.Sessions, variables.StandardTTL, true},
		{"Passkeys", repoRedis.Passkeys, variables.StandardTTL, true},
		{"SessionIndexes", repoRedis.SessionIndexes, variables.StandardTTL, true},
		{"SessionBlacklist", repoRedis.SessionBlacklist, variables.ShortTTL, false},
		{"PreAuth", repoRedis.PreAuth, variables.TwoFactorPreAuthTTL, false},

		// Famille 3 : SPEED Cache (10)
		{"UsersLite", repoRedis.UsersLite, variables.NoTTL, false},
		{"UsersLex", repoRedis.UsersLex, variables.NoTTL, false},
		{"CommunitiesLex", repoRedis.CommunitiesLex, variables.NoTTL, false},
		{"TagsLex", repoRedis.TagsLex, variables.NoTTL, false},
		{"SpeedRelationsIndex", repoRedis.SpeedRelationsIndex, variables.NoTTL, false},
		{"SpeedRelations", repoRedis.SpeedRelations, variables.NoTTL, false},
		{"ConvMeta", repoRedis.ConvMeta, variables.StandardTTL, true},
		{"ConvMembers", repoRedis.ConvMembers, variables.StandardTTL, true},
		{"SpeedAddable", repoRedis.SpeedAddable, variables.StandardTTL, true},
		{"SpeedCommunity", repoRedis.SpeedCommunity, variables.StandardTTL, true},

		// Famille 4 : FEED Cache & Cuckoo (6)
		{"FeedsObject", repoRedis.FeedsObject, variables.ShortTTL, false},
		{"FeedsPersonalized", repoRedis.FeedsPersonalized, variables.ShortTTL, false},
		{"FeedsMailbox", repoRedis.FeedsMailbox, variables.StandardTTL, true},
		{"FeedSchedule", repoRedis.FeedSchedule, variables.NoTTL, false},
		{"CuckooSeen", repoRedis.CuckooSeen, variables.ShortTTL, false},
		{"FanOutAuthorWindows", repoRedis.FanOutAuthorWindows, variables.ShortTTL, false},

		// Famille 5 : Algorithme, MOST Cache, Tags & LSH (15)
		{"ContentVectors", repoRedis.ContentVectors, variables.StandardTTL, true},
		{"LSHBuckets", repoRedis.LSHBuckets, variables.StandardTTL, true},
		{"RankedPosts", repoRedis.RankedPosts, variables.NoTTL, false},
		{"TagPosts", repoRedis.TagPosts, variables.NoTTL, false},
		{"TrendGlobalHourly", repoRedis.TrendGlobalHourly, variables.TrendGlobalHourlyTTL, false},
		{"TrendGlobalDaily", repoRedis.TrendGlobalDaily, variables.TrendGlobalDailyTTL, false},
		{"TrendTagDaily", repoRedis.TrendTagDaily, variables.TrendTagDailyTTL, false},
		{"TrendTagWeekly", repoRedis.TrendTagWeekly, variables.TrendTagWeeklyTTL, false},
		{"HashtagLeaderboard", repoRedis.HashtagLeaderboard, variables.NoTTL, false},
		{"Tags", repoRedis.Tags, variables.NoTTL, false},
		{"HashtagCanon", repoRedis.HashtagCanon, variables.NoTTL, false},
		{"GraphEdges", repoRedis.GraphEdges, variables.NoTTL, false},
		{"TagQuarantine", repoRedis.TagQuarantine, variables.ShortTTL, false},
		{"IndirectTagConsensus", repoRedis.IndirectTagConsensus, variables.StandardTTL, true},
		{"AuthorPostWindows", repoRedis.AuthorPostWindows, variables.AuthorPostWindowTTL, false},

		// Famille 6 : Interactions & Idempotence (2)
		{"PostLikesSet", repoRedis.PostLikesSet, variables.LongTTL, false},
		{"CommentLikesSet", repoRedis.CommentLikesSet, variables.LongTTL, false},

		// Famille 7 : Messagerie & Sync Inbox (7)
		{"ConvParticipants", repoRedis.ConvParticipants, variables.NoTTL, false},
		{"UserInbox", repoRedis.UserInbox, variables.NoTTL, false},
		{"InboxActivity", repoRedis.InboxActivity, variables.NoTTL, false},
		{"MessagesIndex", repoRedis.MessagesIndex, variables.StandardTTL, true},
		{"ConvWatermarks", repoRedis.ConvWatermarks, variables.StandardTTL, true},
		{"UserSyncLedger", repoRedis.UserSyncLedger, variables.LongTTL, false},
		{"ConvMessageLedger", repoRedis.ConvMessageLedger, variables.LongTTL, false},

		// Famille 8 : Notifications & Activité (3)
		{"NotificationsZSet", repoRedis.NotificationsZSet, variables.StandardTTL, true},
		{"NotificationCursors", repoRedis.NotificationCursors, variables.NoTTL, false},
		{"NotificationActivity", repoRedis.NotificationActivity, variables.NoTTL, false},

		// Famille 9 : Recommandations Utilisateurs & Communautés (13)
		{"UserRecoListGeo", repoRedis.UserRecoListGeo, variables.UserRecoListTTL, false},
		{"UserRecoListAff", repoRedis.UserRecoListAff, variables.UserRecoListTTL, false},
		{"UserRecoListTaste", repoRedis.UserRecoListTaste, variables.UserRecoListTTL, false},
		{"UserRecoListGlobal", repoRedis.UserRecoListGlobal, variables.UserRecoListTTL, false},
		{"UsersLang", repoRedis.UsersLang, variables.NoTTL, false},
		{"CuckooUserRecoSkips", repoRedis.CuckooUserRecoSkips, variables.StandardTTL, true},
		{"LockUserRecoCompute", repoRedis.LockUserRecoCompute, variables.UserRecoComputeLockTTL, false},
		{"CommunityRecoRanked", repoRedis.CommunityRecoRanked, variables.NoTTL, false},
		{"CommunityRecoPins", repoRedis.CommunityRecoPins, variables.NoTTL, false},
		{"CommunityRecoMsgCount", repoRedis.CommunityRecoMsgCount, variables.CommunityRecoTelemetryTTL, false},
		{"CommunityRecoAuthors", repoRedis.CommunityRecoAuthors, variables.CommunityRecoTelemetryTTL, false},
		{"CommunityRecoGrowth", repoRedis.CommunityRecoGrowth, variables.CommunityRecoTelemetryTTL, false},
		{"UserPublicComms", repoRedis.UserPublicComms, variables.NoTTL, false},

		// Famille 10 : Modération, Trust Score & Carte Discovery (7)
		{"ReportAggregators", repoRedis.ReportAggregators, variables.StandardTTL, true},
		{"SteganoBanlist", repoRedis.SteganoBanlist, variables.NoTTL, false},
		{"ModerationQueues", repoRedis.ModerationQueues, variables.NoTTL, false},
		{"ModerationExpirations", repoRedis.ModerationExpirations, variables.NoTTL, false},
		{"ModerationTickets", repoRedis.ModerationTickets, variables.NoTTL, false},
		{"ModeratorStats", repoRedis.ModeratorStats, variables.NoTTL, false},
		{"POIsGeo", repoRedis.POIsGeo, variables.NoTTL, false},

		// Famille 11 : Télémétrie, Système, Queues & Scheduler (9)
		{"TelemetryProfiles", repoRedis.TelemetryProfiles, variables.StandardTTL, true},
		{"RateLimits", repoRedis.RateLimits, variables.UltraShortTTL, false},
		{"CronLocks", repoRedis.CronLocks, variables.VeryShortTTL, false},
		{"InfraTelemetry", repoRedis.InfraTelemetry, variables.MinuteTTL, false},
		{"SchedulerState", repoRedis.SchedulerState, variables.NoTTL, false},
		{"DynamicVariables", repoRedis.DynamicVariables, variables.NoTTL, false},
		{"WorkerQueue", repoRedis.WorkerQueue, variables.NoTTL, false},
		{"DLQ", repoRedis.DLQ, variables.NoTTL, false},
		{"SystemStatus", repoRedis.SystemStatus, variables.NoTTL, false},

		// Famille 12 : Bus Pub/Sub & Présence Temps Réel (5)
		{"Presence", repoRedis.Presence, variables.MinuteTTL, false},
		{"ChannelUser", repoRedis.ChannelUser, variables.NoTTL, false},
		{"ChannelCommunity", repoRedis.ChannelCommunity, variables.NoTTL, false},
		{"CuckooSync", repoRedis.CuckooSync, variables.UltraShortTTL, false},
		{"DynamicVariablesSync", repoRedis.DynamicVariablesSync, variables.UltraShortTTL, false},
	}

	require.Len(t, specs, 101, "all 101 Redis collections must be covered")

	for _, s := range specs {
		require.NotNil(t, s.col, "collection %s must be initialized", s.name)
		assert.Equal(t, s.expectedTTL, s.col.DefaultTTL, "unexpected initial DefaultTTL for %s", s.name)
	}

	// Vérifie que RefreshStandardTTL met à jour exactement les 35 collections StandardTTL
	// et ne touche aucune autre collection.
	origTTL := variables.StandardTTL
	newTTL := 96 * time.Hour
	repoRedis.RefreshStandardTTL(newTTL)
	defer repoRedis.RefreshStandardTTL(origTTL)

	standardCount := 0
	for _, s := range specs {
		if s.usesStandardTT {
			standardCount++
			assert.Equal(t, newTTL, s.col.DefaultTTL, "RefreshStandardTTL must update %s", s.name)
		} else {
			assert.Equal(t, s.expectedTTL, s.col.DefaultTTL, "RefreshStandardTTL must NOT modify %s", s.name)
		}
	}
	assert.Equal(t, 35, standardCount)
}

// TestCollectionsHasNoHardcodedDurationLiterals garantit qu'aucun littéral time.Second /
// time.Minute / time.Hour n'est hardcodé dans collections.go (tous les TTL viennent de variables/).
func TestCollectionsHasNoHardcodedDurationLiterals(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "collections.go", nil, 0)
	require.NoError(t, err)

	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != "time" {
			return true
		}
		switch sel.Sel.Name {
		case "Nanosecond", "Microsecond", "Millisecond", "Second", "Minute", "Hour":
			pos := fset.Position(sel.Pos())
			t.Errorf("hardcoded duration unit time.%s found in collections.go:%d — centralize it in internal/variables/redis_ttl_constants.go", sel.Sel.Name, pos.Line)
		}
		return true
	})
}
