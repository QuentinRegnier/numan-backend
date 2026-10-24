package object_cache_service

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/comment_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
)

func TestCommentObjectCacheAndZSET(t *testing.T) {
	srv := test.SetupTestRedis(t)
	ctx := context.Background()

	// Cache miss returns 404 CodeNotFound
	_, errMiss := GetCommentFromObjectCache(ctx, 9999)
	assertAppError(t, errMiss, http.StatusNotFound, numan_error.CodeNotFound)

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

	// Best-effort EXPIRE failure in AddCommentToZSET & IncrementCommentScoreInZSET still returns nil
	srv.SetFailCmd("EXPIRE", errors.New("expire err"))
	if err := AddCommentToZSET(ctx, postID, 101, 10.0); err != nil {
		t.Fatalf("AddCommentToZSET with EXPIRE failure should still succeed, got: %v", err)
	}
	if err := IncrementCommentScoreInZSET(ctx, postID, 101, 25.0); err != nil {
		t.Fatalf("IncrementCommentScoreInZSET with EXPIRE failure should still succeed, got: %v", err)
	}
	srv.SetFailCmd("EXPIRE", nil)

	topIDs, err := GetTopCommentIDs(ctx, postID, 0, 10)
	if err != nil {
		t.Fatalf("GetTopCommentIDs failed: %v", err)
	}
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
	_, errAfterPurge := GetCommentFromObjectCache(ctx, 101)
	assertAppError(t, errAfterPurge, http.StatusNotFound, numan_error.CodeNotFound)

	topIDsAfterPurge, _ := GetTopCommentIDs(ctx, postID, 0, 10)
	if len(topIDsAfterPurge) != 0 {
		t.Errorf("expected empty ZSET after PurgePostCommentsFromL1, got %v", topIDsAfterPurge)
	}

	// Error branches
	srv.SetFailCmd("GET", errors.New("get err"))
	_, errGet := GetCommentFromObjectCache(ctx, 101)
	assertAppError(t, errGet, http.StatusInternalServerError, numan_error.CodeCacheError)
	srv.SetFailCmd("GET", nil)

	srv.SetFailCmd("SET", errors.New("set err"))
	assertAppError(t, SetCommentInObjectCache(ctx, c1), http.StatusInternalServerError, numan_error.CodeCacheError)
	srv.SetFailCmd("SET", nil)

	srv.SetFailCmd("DEL", errors.New("del err"))
	assertAppError(t, DeleteCommentFromObjectCache(ctx, 101), http.StatusInternalServerError, numan_error.CodeCacheError)
	srv.SetFailCmd("DEL", nil)

	srv.SetFailCmd("ZREVRANGE", errors.New("zrevrange err"))
	_, errTop := GetTopCommentIDs(ctx, postID, 0, 10)
	assertAppError(t, errTop, http.StatusInternalServerError, numan_error.CodeCacheError)
	PurgePostCommentsFromL1(ctx, postID) // exercises ZREVRANGE warn branch
	srv.SetFailCmd("ZREVRANGE", nil)

	srv.SetFailCmd("ZREM", errors.New("zrem err"))
	assertAppError(t, RemoveCommentFromZSET(ctx, postID, 101), http.StatusInternalServerError, numan_error.CodeCacheError)
	srv.SetFailCmd("ZREM", nil)

	srv.SetFailCmd("ZINCRBY", errors.New("zincrby err"))
	assertAppError(t, IncrementCommentScoreInZSET(ctx, postID, 101, 1.0), http.StatusInternalServerError, numan_error.CodeCacheError)
	srv.SetFailCmd("ZINCRBY", nil)
}
