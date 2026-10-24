package object_cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoMongo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/lib/pq"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestGetPostsView_EmptyInput(t *testing.T) {
	test.SetupTestRedis(t)
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
	ctx := context.Background()
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("L1, L2, L3 cascade, corrupt MsgPack fallback, and MGET error fallback", func(mt *mtest.T) {
		srv := test.SetupTestRedis(mt.T)
		restore := test.ResetCollections()
		defer restore()
		repoMongo.Posts = test.NewMockCollection(mt, "content.posts", nil)

		// Post 10: stored in L1 Redis
		if err := SetPostInObjectCache(ctx, post_models.PostPayload{
			ID:      10,
			UserID:  1,
			Content: "L1 Post 10",
		}); err != nil {
			mt.Fatalf("SetPostInObjectCache failed: %v", err)
		}

		// Post 20: corrupt MsgPack in L1 -> falls back to L2
		_ = repoRedis.Posts.SetPrimitive(ctx, int64(20), "\xff\xfe\xfdcorrupt")

		// Post 20: stored in L2 MongoDB ("content.posts")
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.content.posts", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(20)},
			{Key: "user_id", Value: int64(2)},
			{Key: "content", Value: "L2 Post 20"},
		}))

		// Post 30: stored in L3 PostgreSQL
		hashtagsVal, _ := pq.Array([]string{"go"}).Value()
		indirectVal, _ := pq.Array([]string{}).Value()
		identVal, _ := pq.Array([]int64{}).Value()
		mediaVal, _ := pq.Array([]int64{}).Value()
		vecVal, _ := pq.Array([]float32{}).Value()
		now := time.Now().UnixMilli()
		cols := []string{
			"id", "user_id", "content", "hashtags", "indirect_hashtags",
			"identifiers", "media_ids", "visibility", "priority_level", "location",
			"created_at", "updated_at", "like_count", "comment_count", "view_count",
			"report_count", "has_media", "vector", "vector_version", "telemetry_dwell_sum",
			"telemetry_dwell_sq", "telemetry_clicks",
		}

		test.SetupMockDB(mt.T, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{{
				int64(30), int64(3), "L3 Post 30", hashtagsVal, indirectVal,
				identVal, mediaVal, int64(1), int64(0), nil,
				now, now, int64(5), int64(1), int64(50),
				int64(0), false, vecVal, int64(1), float64(0),
				float64(0), int64(0),
			}}), nil
		}, nil)

		// Request in specific order: [30, 10, 20]
		targetIDs := []int64{30, 10, 20}
		posts, err := GetPostsView(ctx, targetIDs)
		if err != nil {
			mt.Fatalf("GetPostsView failed: %v", err)
		}
		if len(posts) != 3 {
			mt.Fatalf("expected 3 hydrated posts, got %d", len(posts))
		}

		for i, expID := range targetIDs {
			if posts[i].ID != expID {
				mt.Errorf("at index %d expected post ID %d, got %d", i, expID, posts[i].ID)
			}
		}

		// Bounded deadline polling for async L2->L1 and L3->L1 promotions
		waitForCondition(mt, 250*time.Millisecond, func() bool {
			return IsPostInObjectCache(ctx, 20) && IsPostInObjectCache(ctx, 30)
		})

		// MGET error fallback to L2
		srv.SetFailCmd("MGET", errors.New("mget failed"))
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.content.posts", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(20)},
			{Key: "user_id", Value: int64(2)},
			{Key: "content", Value: "L2 Post 20"},
		}))
		postsMGetErr, err := GetPostsView(ctx, []int64{20})
		srv.SetFailCmd("MGET", nil)
		if err != nil || len(postsMGetErr) != 1 || postsMGetErr[0].ID != 20 {
			mt.Errorf("expected L2 fallback on MGET error, got %+v (err=%v)", postsMGetErr, err)
		}
	})
}
