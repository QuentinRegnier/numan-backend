package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/lib/pq"
)

func TestSeedMostCache_FullFlow(t *testing.T) {
	srv := setupTestRedis(t)
	setupTestMongo(t)
	ctx := context.Background()

	hashtagsVal, _ := pq.Array([]string{"golang"}).Value()
	indirectVal, _ := pq.Array([]string{}).Value()
	identVal, _ := pq.Array([]int64{}).Value()
	mediaVal, _ := pq.Array([]int64{}).Value()
	vecVal, _ := pq.Array([]float32{}).Value()
	now := time.Now().UnixMilli()

	postRow := []driver.Value{
		int64(1001), int64(10), "Seeded Post", hashtagsVal, indirectVal,
		identVal, mediaVal, int64(1), int64(1), nil,
		now, now, int64(42), int64(7), int64(500),
		int64(0), false, vecVal, int64(1), float64(0),
		float64(0), int64(0),
	}

	paginatedCalls := 0
	failPaginated := false
	setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
		if strings.Contains(query, "func_load_all_tags") {
			return fakeSQLResponse{
				cols: []string{"tag"},
				rows: [][]driver.Value{{"golang"}, {"architecture"}},
			}
		}
		if strings.Contains(query, "func_load_posts_paginated") {
			if failPaginated {
				return fakeSQLResponse{err: errors.New("sql failure")}
			}
			paginatedCalls++
			if paginatedCalls == 1 {
				return fakeSQLResponse{cols: postColumns22(), rows: [][]driver.Value{postRow}}
			}
			return fakeSQLResponse{cols: postColumns22(), rows: [][]driver.Value{}}
		}
		return fakeSQLResponse{cols: postColumns22(), rows: [][]driver.Value{postRow}}
	})

	_ = redis.ZAddWithCap(ctx, "most_cache:trend:test", 99.5, int64(1001), 100)

	if err := SeedMostCache(ctx); err != nil {
		t.Fatalf("SeedMostCache failed: %v", err)
	}

	activeTags, err := redis.Tags.SMembers(ctx, "active")
	if err != nil || len(activeTags) != 2 {
		t.Errorf("expected 2 active tags in Redis, got %v (err=%v)", activeTags, err)
	}

	var elitePost post_models.PostPayload
	elitePost, err = object_cache_service.GetPostFromObjectCache(ctx, 1001)
	if err != nil || elitePost.ID != 1001 {
		t.Errorf("expected elite post 1001 in L1 Object Cache, got %+v (err=%v)", elitePost, err)
	}

	status, err := redis.SystemStatus.Client.Get(ctx, redis.SystemStatus.Key("maintenance")).Result()
	if err != nil || status != "off" {
		t.Errorf("expected maintenance status 'off', got %q (err=%v)", status, err)
	}

	failPaginated = true
	if err := SeedMostCache(ctx); err == nil {
		t.Errorf("expected error when FuncLoadPostsPaginated fails, got nil")
	}
	_ = srv
}

func TestSeedSpeedCache_FullFlow(t *testing.T) {
	setupTestRedis(t)
	ctx := context.Background()

	nowTime := time.Now().UTC()
	setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
		switch {
		case strings.Contains(query, "func_load_users_paginated"):
			return fakeSQLResponse{
				cols: []string{"id", "username", "first_name", "last_name", "profile_picture_id", "bio", "grade", "conversation_permission", "add_group_permission"},
				rows: [][]driver.Value{
					{int64(10), "alice", "Alice", "Smith", int64(1), "Bio", int64(1), int64(0), int64(0)},
				},
			}
		case strings.Contains(query, "func_load_relations_paginated"):
			return fakeSQLResponse{
				cols: []string{"caller_id", "target_id", "state", "created_at"},
				rows: [][]driver.Value{
					{int64(10), int64(20), int64(variables.RelationStateFollow), nowTime},
				},
			}
		case strings.Contains(query, "func_load_active_communities"):
			return fakeSQLResponse{
				cols: []string{"id", "name", "description", "avatar_id", "member_count"},
				rows: [][]driver.Value{
					{int64(500), "Go Devs", "Golang Community", int64(9), int64(42)},
				},
			}
		case strings.Contains(query, "func_load_active_conversations"):
			return fakeSQLResponse{
				cols: []string{"id", "type", "title", "description", "avatar_id", "last_message_id", "settings", "external_link"},
				rows: [][]driver.Value{
					{int64(600), int64(0), "DM", "", int64(0), int64(888), `{}`, `[]`},
				},
			}
		default:
			return fakeSQLResponse{
				cols: []string{"conversation_id", "user_id", "role", "settings", "unread_count", "frozen_message_id", "last_read_message_id", "last_message_id", "joined_at"},
				rows: [][]driver.Value{
					{int64(600), int64(10), int64(0), `{"pinned":-1,"is_muted":0}`, int64(0), int64(0), int64(888), int64(888), nowTime},
				},
			}
		}
	})

	if err := SeedSpeedCache(ctx); err != nil {
		t.Fatalf("SeedSpeedCache failed: %v", err)
	}

	userLite, err := GetUserLite(ctx, 10)
	if err != nil || userLite.ID != 10 || userLite.Username != "alice" || userLite.FirstName != "Alice" {
		t.Errorf("expected user 10 ('alice') in speed cache, got %+v (err=%v)", userLite, err)
	}

	followers, err := GetSpeedRelationsIndex(ctx, 20)
	if err != nil || len(followers) != 1 || followers[0] != 10 {
		t.Errorf("expected user 20 to have follower [10], got %v (err=%v)", followers, err)
	}
	if state := RelationValue(ctx, 20, 10); state != variables.RelationStateFollow {
		t.Errorf("expected RelationValue(target=20, caller=10) == %d, got %d", variables.RelationStateFollow, state)
	}
}

func TestSeedUserCache_FullFlow(t *testing.T) {
	setupTestRedis(t)
	ctx := context.Background()

	nowTime := time.Now().UTC()
	setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
		return fakeSQLResponse{
			cols: []string{"post_id", "user_id", "created_at"},
			rows: [][]driver.Value{
				{int64(777), int64(33), nowTime},
			},
		}
	})

	if err := SeedUserCache(ctx); err != nil {
		t.Fatalf("SeedUserCache failed: %v", err)
	}

	ids, err := redis.UserTimeline.ZRevRange(ctx, int64(33), 0, -1)
	if err != nil || len(ids) != 1 || ids[0] != "777" {
		t.Errorf("expected post 777 in user 33 timeline, got %v (err=%v)", ids, err)
	}
}
