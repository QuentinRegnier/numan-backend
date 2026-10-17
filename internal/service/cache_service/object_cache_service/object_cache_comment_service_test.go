package object_cache_service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/comment_models"
)

func TestCommentObjectCacheAndZSET(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	postID := int64(500)
	c1 := comment_models.CommentPayload{
		ID:         101,
		PostID:     postID,
		UserID:     10,
		Content:    "First",
		Visibility: 1,
		LikeCount:  3,
		Score:      10,
		CreatedAt:  "2025-01-01T10:00:00Z",
		UpdatedAt:  "2025-01-01T10:00:00Z",
	}
	c2 := comment_models.CommentPayload{
		ID:         102,
		PostID:     postID,
		UserID:     11,
		Content:    "Second",
		Visibility: 1,
		LikeCount:  5,
		Score:      20,
		CreatedAt:  "2025-01-01T10:05:00Z",
		UpdatedAt:  "2025-01-01T10:05:00Z",
	}

	if err := SetCommentInObjectCache(ctx, c1); err != nil {
		t.Fatalf("SetCommentInObjectCache(101) failed: %v", err)
	}
	if err := SetCommentInObjectCache(ctx, c2); err != nil {
		t.Fatalf("SetCommentInObjectCache(102) failed: %v", err)
	}

	got1, err := GetCommentFromObjectCache(ctx, 101)
	if err != nil || got1 != c1 {
		t.Fatalf("GetCommentFromObjectCache(101) = %+v, want %+v (err=%v)", got1, c1, err)
	}

	// Add comments to ZSET and verify score ranking
	if err := AddCommentToZSET(ctx, postID, 101, 10.0); err != nil {
		t.Fatalf("AddCommentToZSET(101) failed: %v", err)
	}
	if err := AddCommentToZSET(ctx, postID, 102, 20.0); err != nil {
		t.Fatalf("AddCommentToZSET(102) failed: %v", err)
	}

	topIDs, err := GetTopCommentIDs(ctx, postID, 0, 10)
	if err != nil {
		t.Fatalf("GetTopCommentIDs failed: %v", err)
	}
	if !reflect.DeepEqual(topIDs, []int64{102, 101}) {
		t.Errorf("GetTopCommentIDs = %v, want [102 101]", topIDs)
	}

	// IncrementCommentScoreInZSET boosts 101 above 102
	if err := IncrementCommentScoreInZSET(ctx, postID, 101, 25.0); err != nil {
		t.Fatalf("IncrementCommentScoreInZSET failed: %v", err)
	}
	topIDs, _ = GetTopCommentIDs(ctx, postID, 0, 10)
	if !reflect.DeepEqual(topIDs, []int64{101, 102}) {
		t.Errorf("GetTopCommentIDs after increment = %v, want [101 102]", topIDs)
	}

	// RemoveCommentFromZSET
	if err := RemoveCommentFromZSET(ctx, postID, 102); err != nil {
		t.Fatalf("RemoveCommentFromZSET failed: %v", err)
	}
	topIDs, _ = GetTopCommentIDs(ctx, postID, 0, 10)
	if !reflect.DeepEqual(topIDs, []int64{101}) {
		t.Errorf("GetTopCommentIDs after remove = %v, want [101]", topIDs)
	}

	// PurgePostCommentsFromL1 deletes both comment objects and the ZSET
	PurgePostCommentsFromL1(ctx, postID)
	if _, err := GetCommentFromObjectCache(ctx, 101); err == nil {
		t.Errorf("expected comment 101 purged from Object Cache, got nil error")
	}
	topIDsAfterPurge, _ := GetTopCommentIDs(ctx, postID, 0, 10)
	if len(topIDsAfterPurge) != 0 {
		t.Errorf("expected empty ZSET after PurgePostCommentsFromL1, got %v", topIDsAfterPurge)
	}

	// Error branches
	srv.setFailCmd("SET", errors.New("set err"))
	if err := SetCommentInObjectCache(ctx, c1); err == nil {
		t.Errorf("expected error when SET fails")
	}
	srv.setFailCmd("SET", nil)

	srv.setFailCmd("DEL", errors.New("del err"))
	if err := DeleteCommentFromObjectCache(ctx, 101); err == nil {
		t.Errorf("expected error when DEL fails")
	}
	srv.setFailCmd("DEL", nil)

	srv.setFailCmd("ZREVRANGE", errors.New("zrevrange err"))
	if _, err := GetTopCommentIDs(ctx, postID, 0, 10); err == nil {
		t.Errorf("expected error when ZREVRANGE fails")
	}
	srv.setFailCmd("ZREVRANGE", nil)

	srv.setFailCmd("ZREM", errors.New("zrem err"))
	if err := RemoveCommentFromZSET(ctx, postID, 101); err == nil {
		t.Errorf("expected error when ZREM fails")
	}
	srv.setFailCmd("ZREM", nil)

	srv.setFailCmd("ZINCRBY", errors.New("zincrby err"))
	if err := IncrementCommentScoreInZSET(ctx, postID, 101, 1.0); err == nil {
		t.Errorf("expected error when ZINCRBY fails")
	}
	srv.setFailCmd("ZINCRBY", nil)
}
