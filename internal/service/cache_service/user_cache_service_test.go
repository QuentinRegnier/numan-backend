package cache_service

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

func TestUserTimelineLifecycle(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("GetTopUserPostIDs returns error on cache miss (non-existent ZSET)", func(t *testing.T) {
		_, err := GetTopUserPostIDs(ctx, 404, 0, 10)
		if err == nil {
			t.Fatal("GetTopUserPostIDs(404) error = nil, want non-nil on cache miss")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("GetTopUserPostIDs(404) error = %v, want 500 AppError", err)
		}
	})

	t.Run("MarkUserTimelineEmpty seals cache hit and GetTopUserPostIDs returns empty list without error", func(t *testing.T) {
		userID := int64(50)
		if err := MarkUserTimelineEmpty(ctx, userID); err != nil {
			t.Fatalf("MarkUserTimelineEmpty() unexpected error: %v", err)
		}

		ids, err := GetTopUserPostIDs(ctx, userID, 0, 10)
		if err != nil {
			t.Fatalf("GetTopUserPostIDs() on empty-marked timeline error: %v", err)
		}
		if len(ids) != 0 {
			t.Errorf("GetTopUserPostIDs() on empty-marked timeline = %v, want empty", ids)
		}

		if ttl := srv.getTTL(repoRedis.UserTimeline.Key(userID)); ttl != variables.StandardTTL {
			t.Errorf("UserTimeline TTL = %v, want %v", ttl, variables.StandardTTL)
		}
	})

	t.Run("AddPostToUserProfile removes empty marker and orders posts by descending timestamp", func(t *testing.T) {
		userID := int64(60)
		_ = MarkUserTimelineEmpty(ctx, userID)

		if err := AddPostToUserProfile(ctx, userID, 101, 1000.0); err != nil {
			t.Fatalf("AddPostToUserProfile(101) error: %v", err)
		}
		if err := AddPostToUserProfile(ctx, userID, 102, 3000.0); err != nil {
			t.Fatalf("AddPostToUserProfile(102) error: %v", err)
		}
		if err := AddPostToUserProfile(ctx, userID, 103, 2000.0); err != nil {
			t.Fatalf("AddPostToUserProfile(103) error: %v", err)
		}

		got, err := GetTopUserPostIDs(ctx, userID, 0, 10)
		if err != nil {
			t.Fatalf("GetTopUserPostIDs() error: %v", err)
		}
		want := []int64{102, 103, 101}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("GetTopUserPostIDs() = %v, want %v", got, want)
		}

		// Remove post 103
		if err := RemovePostFromUserProfile(ctx, userID, 103); err != nil {
			t.Fatalf("RemovePostFromUserProfile(103) error: %v", err)
		}

		gotAfterRem, err := GetTopUserPostIDs(ctx, userID, 0, 10)
		if err != nil {
			t.Fatalf("GetTopUserPostIDs() after remove error: %v", err)
		}
		wantAfterRem := []int64{102, 101}
		if !reflect.DeepEqual(gotAfterRem, wantAfterRem) {
			t.Errorf("GetTopUserPostIDs() after remove = %v, want %v", gotAfterRem, wantAfterRem)
		}

		// Purge timeline completely
		if err := PurgeUserTimeline(ctx, userID); err != nil {
			t.Fatalf("PurgeUserTimeline() error: %v", err)
		}

		if _, err := GetTopUserPostIDs(ctx, userID, 0, 10); err == nil {
			t.Fatal("GetTopUserPostIDs() after purge error = nil, want cache miss error")
		}
	})

	t.Run("error propagation on Redis command failures", func(t *testing.T) {
		userID := int64(70)
		_ = AddPostToUserProfile(ctx, userID, 1, 100)

		srv.setFailCmd("ZREVRANGE", errors.New("zrevrange error"))
		if _, err := GetTopUserPostIDs(ctx, userID, 0, 10); err == nil {
			t.Error("GetTopUserPostIDs() expected error on ZREVRANGE failure")
		}
		srv.setFailCmd("ZREVRANGE", nil)

		srv.setFailCmd("ZADD", errors.New("zadd error"))
		if err := MarkUserTimelineEmpty(ctx, userID); err == nil {
			t.Error("MarkUserTimelineEmpty() expected error on ZADD failure")
		}
		if err := AddPostToUserProfile(ctx, userID, 2, 200); err == nil {
			t.Error("AddPostToUserProfile() expected error on ZADD failure")
		}
		srv.setFailCmd("ZADD", nil)

		srv.setFailCmd("ZREM", errors.New("zrem error"))
		if err := RemovePostFromUserProfile(ctx, userID, 1); err == nil {
			t.Error("RemovePostFromUserProfile() expected error on ZREM failure")
		}
		srv.setFailCmd("ZREM", nil)

		srv.setFailCmd("DEL", errors.New("del error"))
		if err := PurgeUserTimeline(ctx, userID); err == nil {
			t.Error("PurgeUserTimeline() expected error on DEL failure")
		}
		srv.setFailCmd("DEL", nil)
	})
}
