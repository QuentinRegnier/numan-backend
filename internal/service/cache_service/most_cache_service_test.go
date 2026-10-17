package cache_service

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/lib/pq"
	"go.mongodb.org/mongo-driver/bson"
)

func TestEvaluatePostAfterLikeAndView(t *testing.T) {
	setupTestRedis(t)
	ctx := context.Background()

	if err := redis.Tags.SAdd(ctx, "active", "golang", "testing"); err != nil {
		t.Fatalf("redis.Tags.SAdd failed: %v", err)
	}

	nowMs := time.Now().Add(-10 * time.Minute).UnixMilli()
	post := post_models.PostPayload{
		ID:               101,
		UserID:           10,
		Content:          "Hello world",
		Hashtags:         []string{"golang"},
		IndirectHashtags: []string{"testing"},
		Identifiers:      []int64{},
		MediaIDs:         []int64{1},
		LikeCount:        25,
		CommentCount:     5,
		ViewCount:        200,
		HasMedia:         true,
		Visibility:       1,
		PriorityLevel:    2,
		Location:         "Paris",
		CreatedAt:        nowMs,
		UpdatedAt:        nowMs,
	}

	EvaluatePostAfterLike(ctx, post)
	EvaluatePostAfterView(ctx, post)

	likesList, err := redis.ZRange(ctx, variables.RedisKeyStrictLikes, 0, -1)
	if err != nil || len(likesList) != 1 || likesList[0] != "101" {
		t.Errorf("expected post 101 in StrictLikes, got %v (err=%v)", likesList, err)
	}

	viewsList, err := redis.ZRange(ctx, variables.RedisKeyStrictViews, 0, -1)
	if err != nil || len(viewsList) != 1 || viewsList[0] != "101" {
		t.Errorf("expected post 101 in StrictViews, got %v (err=%v)", viewsList, err)
	}

	recentList, err := redis.ZRange(ctx, variables.RedisKeyStrictRecent, 0, -1)
	if err != nil || len(recentList) != 1 || recentList[0] != "101" {
		t.Errorf("expected post 101 in StrictRecent, got %v (err=%v)", recentList, err)
	}

	leaderboard, err := redis.HashtagLeaderboard.ZRevRange(ctx, "global", 0, -1)
	if err != nil || len(leaderboard) < 2 {
		t.Errorf("expected hashtags in leaderboard, got %v (err=%v)", leaderboard, err)
	}
}

func TestGetRankedPosts_L1Hit(t *testing.T) {
	setupTestRedis(t)
	ctx := context.Background()

	nowMs := time.Now().UnixMilli()
	post := post_models.PostPayload{
		ID:         202,
		UserID:     20,
		Content:    "Ranked L1 Post",
		Hashtags:   []string{"golang"},
		Visibility: 1,
		LikeCount:  99,
		ViewCount:  500,
		CreatedAt:  nowMs,
		UpdatedAt:  nowMs,
	}
	if err := object_cache_service.SetPostInObjectCache(ctx, post); err != nil {
		t.Fatalf("SetPostInObjectCache failed: %v", err)
	}
	if err := redis.RankedPosts.ZAddWithCap(ctx, "likes:strict", 99, post.ID, variables.MaxStrictElements); err != nil {
		t.Fatalf("ZAddWithCap failed: %v", err)
	}

	posts, err := GetRankedPosts(ctx, "likes:strict", 0, 10)
	if err != nil {
		t.Fatalf("GetRankedPosts failed: %v", err)
	}
	if len(posts) != 1 || posts[0].ID != 202 || posts[0].LikeCount != 99 {
		t.Fatalf("expected 1 post with ID 202 and LikeCount 99, got %+v", posts)
	}
}

func TestGetRankedPosts_BeyondRAM_L2MongoAndL3Fallback(t *testing.T) {
	setupTestRedis(t)
	mongoSrv := setupTestMongo(t)
	ctx := context.Background()

	nowMs := time.Now().UnixMilli()
	mongoSrv.setDocs("content.posts", bson.M{
		"id":         int64(303),
		"user_id":    int64(30),
		"content":    "Cold Ranked Post",
		"visibility": int32(1),
		"like_count": int32(500),
		"view_count": int32(1200),
		"created_at": nowMs,
		"updated_at": nowMs,
	})

	posts, err := GetRankedPosts(ctx, "strict:likes", int64(variables.MaxStrictElements), 10)
	if err != nil {
		t.Fatalf("GetRankedPosts (L2) failed: %v", err)
	}
	if len(posts) != 1 || posts[0].ID != 303 || posts[0].LikeCount != 500 {
		t.Fatalf("expected post 303 from Mongo L2, got %+v", posts)
	}

	mongoSrv.setFailCollection("content.posts", true)
	setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
		return fakeSQLResponse{
			cols: []string{"id"},
			rows: [][]driver.Value{{int64(404)}},
		}
	})
	if err := object_cache_service.SetPostInObjectCache(ctx, post_models.PostPayload{
		ID:         404,
		UserID:     40,
		Content:    "Postgres Ranked Fallback Post",
		Visibility: 1,
		ViewCount:  900,
		CreatedAt:  nowMs,
		UpdatedAt:  nowMs,
	}); err != nil {
		t.Fatalf("SetPostInObjectCache failed: %v", err)
	}

	pgPosts, err := GetRankedPosts(ctx, "strict:views", int64(variables.MaxStrictElements), 10)
	if err != nil {
		t.Fatalf("GetRankedPosts (L3 fallback) failed: %v", err)
	}
	if len(pgPosts) != 1 || pgPosts[0].ID != 404 || pgPosts[0].ViewCount != 900 {
		t.Fatalf("expected post 404 from L3 fallback, got %+v", pgPosts)
	}
}

func TestGetTagPosts_L1L2AndL3Fallback(t *testing.T) {
	setupTestRedis(t)
	mongoSrv := setupTestMongo(t)
	ctx := context.Background()

	now := time.Now().UnixMilli()
	postL1 := post_models.PostPayload{
		ID:         501,
		UserID:     50,
		Content:    "Tag L1",
		Hashtags:   []string{"go"},
		Visibility: 1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	_ = object_cache_service.SetPostInObjectCache(ctx, postL1)
	_ = redis.TagPosts.ZAddWithCap(ctx, "go", 100, postL1.ID, variables.MaxTagElements)

	resL1, err := GetTagPosts(ctx, "go", 0, 10)
	if err != nil || len(resL1) != 1 || resL1[0].ID != 501 {
		t.Fatalf("expected post 501 from TagPosts L1, got %+v (err=%v)", resL1, err)
	}

	mongoSrv.setDocs("content.posts", bson.M{
		"id":         int64(502),
		"user_id":    int64(52),
		"content":    "Tag L2 Mongo",
		"hashtags":   bson.A{"go"},
		"visibility": int32(1),
		"created_at": now,
		"updated_at": now,
	})
	resL2, err := GetTagPosts(ctx, "go", int64(variables.MaxTagElements), 10)
	if err != nil || len(resL2) != 1 || resL2[0].ID != 502 {
		t.Fatalf("expected post 502 from Mongo L2, got %+v (err=%v)", resL2, err)
	}

	mongoSrv.setFailCollection("content.posts", true)

	hashtagsVal, _ := pq.Array([]string{"go"}).Value()
	indirectVal, _ := pq.Array([]string{}).Value()
	identVal, _ := pq.Array([]int64{}).Value()
	mediaVal, _ := pq.Array([]int64{}).Value()
	vecVal, _ := pq.Array([]float32{}).Value()

	setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
		if strings.Contains(query, "func_load_post_ids_by_tag_paginated") {
			return fakeSQLResponse{
				cols: []string{"id"},
				rows: [][]driver.Value{{int64(503)}},
			}
		}
		return fakeSQLResponse{
			cols: postColumns22(),
			rows: [][]driver.Value{{
				int64(503), int64(10), "Tag L3 Postgres", hashtagsVal, indirectVal,
				identVal, mediaVal, int64(1), int64(0), nil,
				now, now, int64(1), int64(0), int64(1),
				int64(0), false, vecVal, int64(1), float64(0),
				float64(0), int64(0),
			}},
		}
	})

	resL3, err := GetTagPosts(ctx, "go", int64(variables.MaxTagElements), 10)
	if err != nil || len(resL3) != 1 || resL3[0].ID != 503 {
		t.Fatalf("expected post 503 from Postgres L3 fallback, got %+v (err=%v)", resL3, err)
	}
}
