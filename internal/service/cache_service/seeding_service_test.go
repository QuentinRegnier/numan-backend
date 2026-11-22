package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoMongo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestSeedingService(t *testing.T) {
	ctx := context.Background()

	postColumns := []string{
		"id", "user_id", "content", "hashtags", "indirect_hashtags", "identifiers", "media_ids",
		"visibility", "priority_level", "location", "created_at", "updated_at",
		"like_count", "comment_count", "view_count", "report_count", "has_media",
		"vector", "vector_version", "telemetry_dwell_sum", "telemetry_dwell_sq", "telemetry_clicks",
	}
	samplePostRow := []driver.Value{
		int64(101), int64(1), "Hello Seed", `{golang}`, `{redis}`, `{}`, `{}`,
		0, 0, "Paris", int64(1700000000000), int64(1700000000000),
		10, 2, 50, 0, false,
		`{0.1,0.2}`, 1, int64(0), int64(0), int64(0),
	}

	t.Run("SeedMostCache full lifecycle", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
		restore := test.ResetCollections()
		defer restore()

		mt.Run("success path", func(mt *mtest.T) {
			repoMongo.Posts = test.NewMockCollection(mt, "content.posts", nil)
			mt.AddMockResponses(mtest.CreateSuccessResponse())

			paginatedCalls := 0
			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				switch {
				case strings.Contains(query, "func_load_all_tags"):
					return test.NewMockRows([]string{"slug"}, [][]driver.Value{{"golang"}, {"redis"}}), nil
				case strings.Contains(query, "func_load_posts_paginated"):
					paginatedCalls++
					if paginatedCalls == 1 {
						return test.NewMockRows(postColumns, [][]driver.Value{samplePostRow}), nil
					}
					return test.NewMockRows(postColumns, nil), nil
				case strings.Contains(query, "func_load_posts("):
					return test.NewMockRows(postColumns, [][]driver.Value{samplePostRow}), nil
				case strings.Contains(query, "func_load_recent_posts"):
					return test.NewMockRows(postColumns, [][]driver.Value{samplePostRow}), nil
				default:
					return test.NewMockRows([]string{"id"}, nil), nil
				}
			}, nil)

			require.NoError(t, SeedMostCache(ctx))

			exists, err := redis.SystemStatus.Exists(ctx, "maintenance")
			require.NoError(t, err)
			assert.True(t, exists)
		})
	})

	t.Run("SeedMostCache error paths", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		var appErr *numan_error.AppError

		// 1. SAdd failure on tags
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows([]string{"slug"}, [][]driver.Value{{"golang"}}), nil
		}, nil)
		srv.SetFailCmd("SADD", errors.New("sadd err"))
		require.ErrorAs(t, SeedMostCache(ctx), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("SADD", nil)

		// 2. FuncLoadPostsPaginated error propagated directly
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "func_load_all_tags") {
				return nil, errors.New("tags pg err")
			}
			return nil, errors.New("posts pg err")
		}, nil)
		require.ErrorAs(t, SeedMostCache(ctx), &appErr)
		assert.Equal(t, numan_error.CodeDatabaseError, appErr.Code)

		// 3. SCAN failure
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(postColumns, nil), nil
		}, nil)
		srv.SetFailCmd("SCAN", errors.New("scan err"))
		require.ErrorAs(t, SeedMostCache(ctx), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("SCAN", nil)

		// 4. Maintenance SetPrimitive failure
		srv.SetFailCmd("SET", errors.New("set err"))
		require.ErrorAs(t, SeedMostCache(ctx), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("SET", nil)
	})

	t.Run("SeedSpeedCache and SeedUserCache", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		now := domain.NowTime()

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			switch {
			case strings.Contains(query, "func_load_users_paginated"):
				return test.NewMockRows(
					[]string{"id", "username", "first_name", "last_name", "profile_picture_id", "bio", "grade", "conversation_permission", "add_group_permission"},
					[][]driver.Value{{int64(1), "alice", "Alice", "A", int64(10), "Bio", 0, 0, 0}},
				), nil
			case strings.Contains(query, "func_load_relations_paginated"):
				return test.NewMockRows(
					[]string{"caller_id", "target_id", "state", "created_at"},
					[][]driver.Value{{int64(1), int64(2), 1, now}},
				), nil
			case strings.Contains(query, "func_load_active_communities"):
				return test.NewMockRows(
					[]string{"id", "name", "description", "avatar_id", "member_count"},
					[][]driver.Value{{int64(50), "Numan Comm", "Desc", int64(0), 5}},
				), nil
			case strings.Contains(query, "func_load_active_conversations"):
				return test.NewMockRows(
					[]string{"id", "type", "title", "description", "avatar_id", "last_message_id", "settings", "external_link", "lang", "is_partner", "pin_slot"},
					nil,
				), nil
			case strings.Contains(query, "func_load_active_members"):
				return test.NewMockRows(
					[]string{"conversation_id", "user_id", "role", "settings", "unread_count", "frozen_message_id", "last_read_message_id", "last_message_id", "joined_at"},
					nil,
				), nil
			case strings.Contains(query, "func_load_active_community_pins"):
				return test.NewMockRows(
					[]string{"conversation_id", "lang", "pin_slot"},
					[][]driver.Value{{int64(50), "fr", int64(1)}},
				), nil
			case strings.Contains(query, "func_load_active_public_community_memberships"):
				return test.NewMockRows(
					[]string{"user_id", "conversation_id"},
					[][]driver.Value{{int64(1), int64(50)}},
				), nil
			case strings.Contains(query, "func_load_timeline_seed_paginated"):
				return test.NewMockRows(
					[]string{"post_id", "user_id", "created_at"},
					[][]driver.Value{{int64(101), int64(1), now}},
				), nil
			default:
				return test.NewMockRows([]string{"id"}, nil), nil
			}
		}, nil)

		require.NoError(t, SeedSpeedCache(ctx))
		require.NoError(t, SeedUserCache(ctx))

		// Verify community recommendation pins & memberships were seeded by SeedSpeedCache
		pinsFR, errPins := GetCommunityPinsByLang(ctx, "fr")
		require.NoError(t, errPins)
		assert.Equal(t, int64(50), pinsFR[1])

		userComms, errComms := GetUserPublicCommunities(ctx, 1)
		require.NoError(t, errComms)
		assert.Contains(t, userComms, int64(50))

		// Verify user timeline was populated by SeedUserCache
		ids, err := GetTopUserPostIDs(ctx, 1, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []int64{101}, ids)

		// Error resilience in SeedSpeedCache and SeedUserCache when Postgres fails
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("pg down")
		}, nil)
		require.NoError(t, SeedSpeedCache(ctx))
		require.NoError(t, SeedUserCache(ctx))
	})

	t.Run("SeedCommunityRecommendations seeds pins, memberships, purges stale pins, and fires compute hook", func(t *testing.T) {
		_ = test.SetupTestRedis(t)

		// Stale pin in EN should be cleared during seeding
		require.NoError(t, SetCommunityPinInCache(ctx, "en", 3, 999))

		hookCh := make(chan struct{}, 1)
		RegisterCommunityRecoInitialComputeHook(func(_ context.Context) {
			hookCh <- struct{}{}
		})
		t.Cleanup(func() { RegisterCommunityRecoInitialComputeHook(nil) })

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			switch {
			case strings.Contains(query, "func_load_active_community_pins"):
				return test.NewMockRows(
					[]string{"conversation_id", "lang", "pin_slot"},
					[][]driver.Value{
						{int64(301), "fr", int64(1)},
						{int64(302), "fr", int64(4)},
					},
				), nil
			case strings.Contains(query, "func_load_active_public_community_memberships"):
				return test.NewMockRows(
					[]string{"user_id", "conversation_id"},
					[][]driver.Value{
						{int64(10), int64(301)},
						{int64(10), int64(302)},
						{int64(20), int64(301)},
					},
				), nil
			default:
				return test.NewMockRows([]string{"id"}, nil), nil
			}
		}, nil)

		require.NoError(t, SeedCommunityRecommendations(ctx))

		frPins, err := GetCommunityPinsByLang(ctx, "fr")
		require.NoError(t, err)
		assert.Equal(t, map[int]int64{1: 301, 4: 302}, frPins)

		enPins, err := GetCommunityPinsByLang(ctx, "en")
		require.NoError(t, err)
		assert.Empty(t, enPins)

		u10Comms, err := GetUserPublicCommunities(ctx, 10)
		require.NoError(t, err)
		assert.ElementsMatch(t, []int64{301, 302}, u10Comms)

		u20Comms, err := GetUserPublicCommunities(ctx, 20)
		require.NoError(t, err)
		assert.ElementsMatch(t, []int64{301}, u20Comms)

		<-hookCh
	})

	t.Run("SeedArticles populates non-evictable Redis index and L2 Mongo", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
		restore := test.ResetCollections()
		defer restore()

		articleCols := []string{
			"id", "title", "summary", "cover_media_id", "markdown_media_id",
			"attachment_media_ids", "partner_url", "visibility", "expires_at", "created_at", "updated_at",
		}

		mt.Run("success and L3 error", func(mt *mtest.T) {
			repoMongo.Articles = test.NewMockCollection(mt, "content.articles", nil)
			mt.AddMockResponses(mtest.CreateSuccessResponse())

			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "func_load_active_articles") {
					return test.NewMockRows(articleCols, [][]driver.Value{
						{int64(701), "Seeded Article", "Summary", int64(201), int64(301), "{401}", "https://partner.fr", int64(0), nil, int64(1700000000000), int64(1700000000000)},
					}), nil
				}
				return test.NewMockRows(articleCols, nil), nil
			}, nil)

			require.NoError(mt, SeedArticles(ctx))

			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				return nil, errors.New("pg fail")
			}, nil)
			assert.Error(mt, SeedArticles(ctx))
		})
	})
}
