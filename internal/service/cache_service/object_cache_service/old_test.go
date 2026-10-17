package object_cache_service

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/lib/pq"
	"go.mongodb.org/mongo-driver/bson"
)

func TestGetPostsView_EmptyInput(t *testing.T) {
	setupTestRedis(t)
	ctx := context.Background()

	posts, err := GetPostsView(ctx, []int64{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(posts) != 0 {
		t.Fatalf("expected 0 posts, got %d", len(posts))
	}
}

func TestGetPostsView_HybridCascadeAndOrderPreservation(t *testing.T) {
	setupTestRedis(t)
	mongoSrv := setupTestMongo(t)
	ctx := context.Background()

	// Post 10: stored in L1 Redis
	if err := SetPostInObjectCache(ctx, post_models.PostPayload{
		ID:      10,
		UserID:  1,
		Content: "L1 Post 10",
	}); err != nil {
		t.Fatalf("SetPostInObjectCache failed: %v", err)
	}

	// Post 20: stored in L2 MongoDB ("content.posts")
	mongoSrv.setDocs("content.posts", bson.M{
		"id":      int64(20),
		"user_id": int64(2),
		"content": "L2 Post 20",
	})

	// Post 30: stored in L3 PostgreSQL
	hashtagsVal, _ := pq.Array([]string{"go"}).Value()
	indirectVal, _ := pq.Array([]string{}).Value()
	identVal, _ := pq.Array([]int64{}).Value()
	mediaVal, _ := pq.Array([]int64{}).Value()
	vecVal, _ := pq.Array([]float32{}).Value()
	now := time.Now().UnixMilli()

	setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
		return fakeSQLResponse{
			cols: []string{
				"id", "user_id", "content", "hashtags", "indirect_hashtags",
				"identifiers", "media_ids", "visibility", "priority_level", "location",
				"created_at", "updated_at", "like_count", "comment_count", "view_count",
				"report_count", "has_media", "vector", "vector_version", "telemetry_dwell_sum",
				"telemetry_dwell_sq", "telemetry_clicks",
			},
			rows: [][]driver.Value{{
				int64(30), int64(3), "L3 Post 30", hashtagsVal, indirectVal,
				identVal, mediaVal, int64(1), int64(0), nil,
				now, now, int64(5), int64(1), int64(50),
				int64(0), false, vecVal, int64(1), float64(0),
				float64(0), int64(0),
			}},
		}
	})

	// Request in specific order: [30, 10, 20]
	targetIDs := []int64{30, 10, 20}
	posts, err := GetPostsView(ctx, targetIDs)
	if err != nil {
		t.Fatalf("GetPostsView failed: %v", err)
	}
	if len(posts) != 3 {
		t.Fatalf("expected 3 hydrated posts, got %d", len(posts))
	}

	for i, expID := range targetIDs {
		if posts[i].ID != expID {
			t.Errorf("at index %d expected post ID %d, got %d", i, expID, posts[i].ID)
		}
	}

	// Wait briefly for async L2->L1 and L3->L1 promotions
	time.Sleep(30 * time.Millisecond)

	if !IsPostInObjectCache(ctx, 20) {
		t.Errorf("expected post 20 to be promoted to L1 Object Cache")
	}
	if !IsPostInObjectCache(ctx, 30) {
		t.Errorf("expected post 30 to be promoted to L1 Object Cache")
	}
}
