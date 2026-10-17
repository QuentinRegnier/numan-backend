package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"go.mongodb.org/mongo-driver/bson"
)

func postColumns22() []string {
	return []string{
		"id", "user_id", "content", "hashtags", "indirect_hashtags",
		"identifiers", "media_ids", "visibility", "priority_level", "location",
		"created_at", "updated_at", "like_count", "comment_count", "view_count",
		"report_count", "has_media", "vector", "vector_version", "telemetry_dwell_sum",
		"telemetry_dwell_sq", "telemetry_clicks",
	}
}

func TestFetchAndHydrateFromCollection(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	empty, err := fetchAndHydrateFromCollection(ctx, redis.RankedPosts, "recent:strict", 0, 10)
	if err != nil {
		t.Fatalf("unexpected error on empty ZSET: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected 0 posts, got %d", len(empty))
	}

	_ = redis.RankedPosts.ZAddWithCap(ctx, "recent:strict", 10, "invalid_id", 100)
	_ = redis.RankedPosts.ZAddWithCap(ctx, "recent:strict", 20, int64(701), 100)
	_ = object_cache_service.SetPostInObjectCache(ctx, post_models.PostPayload{
		ID:         701,
		UserID:     7,
		Content:    "Hydrated Post 701",
		Hashtags:   []string{"news"},
		Visibility: 1,
		LikeCount:  10,
		CreatedAt:  1700000010000,
		UpdatedAt:  1700000010000,
	})

	posts, err := fetchAndHydrateFromCollection(ctx, redis.RankedPosts, "recent:strict", 0, 10)
	if err != nil {
		t.Fatalf("fetchAndHydrateFromCollection failed: %v", err)
	}
	if len(posts) != 1 || posts[0].ID != 701 || posts[0].Content != "Hydrated Post 701" {
		t.Fatalf("expected 1 hydrated post 701, got %+v", posts)
	}

	srv.setFailCmd("ZREVRANGE", errors.New("zrevrange err"))
	if _, err := fetchAndHydrateFromCollection(ctx, redis.RankedPosts, "recent:strict", 0, 10); err == nil {
		t.Fatalf("expected error when ZREVRANGE fails, got nil")
	}
}

func TestGetPostsFromMongoPaginated(t *testing.T) {
	mongoSrv := setupTestMongo(t)

	mongoSrv.setDocs("content.posts", bson.M{
		"id":         int64(801),
		"user_id":    int64(8),
		"content":    "Mongo Paginated Post",
		"hashtags":   bson.A{"news"},
		"visibility": int32(1),
		"created_at": int64(1700000020000),
		"updated_at": int64(1700000020000),
	})

	posts, err := getPostsFromMongoPaginated("hashtags", "news", 0, 5)
	if err != nil {
		t.Fatalf("getPostsFromMongoPaginated failed: %v", err)
	}
	if len(posts) != 1 || posts[0].ID != 801 || posts[0].Content != "Mongo Paginated Post" {
		t.Fatalf("expected post 801, got %+v", posts)
	}

	mongoSrv.setFailCollection("content.posts", true)
	if _, err := getPostsFromMongoPaginated("hashtags", "news", 0, 5); err == nil {
		t.Fatalf("expected error when Mongo fails, got nil")
	}
}

func TestGetPostsFromPostgresPaginated_AllBranches(t *testing.T) {
	setupTestRedis(t)
	ctx := context.Background()

	for _, id := range []int64{901, 902, 903} {
		_ = object_cache_service.SetPostInObjectCache(ctx, post_models.PostPayload{
			ID:         id,
			UserID:     9,
			Content:    "Post from PG pagination",
			Visibility: 1,
			CreatedAt:  1700000030000,
			UpdatedAt:  1700000030000,
		})
	}

	failNext := false
	setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
		if failNext {
			return fakeSQLResponse{err: errors.New("db down")}
		}
		if strings.Contains(query, "FROM content.likes") {
			return fakeSQLResponse{cols: []string{"id"}, rows: [][]driver.Value{{int64(901)}}}
		}
		if strings.Contains(query, "FROM content.views") {
			return fakeSQLResponse{cols: []string{"id"}, rows: [][]driver.Value{{int64(902)}}}
		}
		return fakeSQLResponse{cols: []string{"id"}, rows: [][]driver.Value{{int64(903)}}}
	})

	likesPosts, err := getPostsFromPostgresPaginated(ctx, "likes:strict", 0, 10)
	if err != nil || len(likesPosts) != 1 || likesPosts[0].ID != 901 {
		t.Errorf("expected post 901 for likes:strict, got %+v (err=%v)", likesPosts, err)
	}

	viewsPosts, err := getPostsFromPostgresPaginated(ctx, "views:strict", 0, 10)
	if err != nil || len(viewsPosts) != 1 || viewsPosts[0].ID != 902 {
		t.Errorf("expected post 902 for views:strict, got %+v (err=%v)", viewsPosts, err)
	}

	defaultPosts, err := getPostsFromPostgresPaginated(ctx, "recent:strict", 0, 10)
	if err != nil || len(defaultPosts) != 1 || defaultPosts[0].ID != 903 {
		t.Errorf("expected post 903 for default rankType, got %+v (err=%v)", defaultPosts, err)
	}

	failNext = true
	if _, err := getPostsFromPostgresPaginated(ctx, "other", 0, 10); err == nil {
		t.Errorf("expected error when Postgres query fails, got nil")
	}
}
