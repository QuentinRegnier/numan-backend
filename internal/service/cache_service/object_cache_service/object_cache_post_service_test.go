package object_cache_service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
)

func TestPostObjectCache_CRUDAndExists(t *testing.T) {
	srv := test.SetupTestRedis(t)
	ctx := context.Background()

	postID := int64(555)
	if IsPostInObjectCache(ctx, postID) {
		t.Fatalf("expected post %d not to exist initially", postID)
	}

	// Cache miss returns 404 CodeNotFound AppError
	_, errMiss := GetPostFromObjectCache(ctx, postID)
	assertAppError(t, errMiss, http.StatusNotFound, numan_error.CodeNotFound)

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

	_, errAfterDel := GetPostFromObjectCache(ctx, postID)
	assertAppError(t, errAfterDel, http.StatusNotFound, numan_error.CodeNotFound)

	// Error paths
	srv.SetFailCmd("GET", errors.New("get failed"))
	_, errGet := GetPostFromObjectCache(ctx, postID)
	assertAppError(t, errGet, http.StatusInternalServerError, numan_error.CodeCacheError)
	srv.SetFailCmd("GET", nil)

	srv.SetFailCmd("SET", errors.New("set failed"))
	assertAppError(t, SetPostInObjectCache(ctx, post), http.StatusInternalServerError, numan_error.CodeCacheError)
	srv.SetFailCmd("SET", nil)

	srv.SetFailCmd("DEL", errors.New("del failed"))
	assertAppError(t, DeletePostFromObjectCache(ctx, postID), http.StatusInternalServerError, numan_error.CodeCacheError)
	srv.SetFailCmd("DEL", nil)

	srv.SetFailCmd("EXISTS", errors.New("exists failed"))
	if IsPostInObjectCache(ctx, postID) {
		t.Errorf("expected false when EXISTS fails")
	}
}
