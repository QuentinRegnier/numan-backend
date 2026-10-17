package object_cache_service

import (
	"context"
	"errors"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
)

func TestPostObjectCache_CRUDAndExists(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	postID := int64(555)
	if IsPostInObjectCache(ctx, postID) {
		t.Fatalf("expected post %d not to exist initially", postID)
	}

	post := post_models.PostPayload{
		ID:         postID,
		UserID:     42,
		Content:    "Unit testing post cache",
		Hashtags:   []string{"golang"},
		Visibility: 1,
		Location:   "Paris",
		LikeCount:  12,
		ViewCount:  150,
		CreatedAt:  1700000010000,
		UpdatedAt:  1700000020000,
	}

	if err := SetPostInObjectCache(ctx, post); err != nil {
		t.Fatalf("SetPostInObjectCache failed: %v", err)
	}

	if !IsPostInObjectCache(ctx, postID) {
		t.Errorf("expected post %d to exist after SetPostInObjectCache", postID)
	}

	got, err := GetPostFromObjectCache(ctx, postID)
	if err != nil {
		t.Fatalf("GetPostFromObjectCache failed: %v", err)
	}
	if got.ID != postID || got.Content != post.Content || got.LikeCount != 12 || got.Location != "Paris" || got.CreatedAt != post.CreatedAt {
		t.Errorf("GetPostFromObjectCache = %+v, want %+v", got, post)
	}

	if err := DeletePostFromObjectCache(ctx, postID); err != nil {
		t.Fatalf("DeletePostFromObjectCache failed: %v", err)
	}

	if IsPostInObjectCache(ctx, postID) {
		t.Errorf("expected post %d not to exist after DeletePostFromObjectCache", postID)
	}

	if _, err := GetPostFromObjectCache(ctx, postID); err == nil {
		t.Errorf("expected cache miss error after deletion, got nil")
	}

	// Error paths
	srv.setFailCmd("SET", errors.New("set failed"))
	if err := SetPostInObjectCache(ctx, post); err == nil {
		t.Errorf("expected error when SET fails, got nil")
	}
	srv.setFailCmd("SET", nil)

	srv.setFailCmd("DEL", errors.New("del failed"))
	if err := DeletePostFromObjectCache(ctx, postID); err == nil {
		t.Errorf("expected error when DEL fails, got nil")
	}
	srv.setFailCmd("DEL", nil)

	srv.setFailCmd("EXISTS", errors.New("exists failed"))
	if IsPostInObjectCache(ctx, postID) {
		t.Errorf("expected false when EXISTS fails")
	}
}
