package object_cache_service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

func TestMediaObjectCache_LifecycleAndSoftDeleteCheck(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	// 1. Visible media is stored and retrieved with all fields preserved
	visibleMedia := media_models.MediaPayload{
		ID:          101,
		OwnerID:     10,
		StoragePath: "s3://numan-media/users/10/post_101.webp",
		Visibility:  true,
		CreatedAt:   1700000001000,
		UpdatedAt:   1700000002000,
	}
	if err := SetMediaInObjectCache(ctx, visibleMedia); err != nil {
		t.Fatalf("SetMediaInObjectCache failed: %v", err)
	}

	got, err := GetMediaFromObjectCache(ctx, 101)
	if err != nil {
		t.Fatalf("GetMediaFromObjectCache failed: %v", err)
	}
	if got.ID != visibleMedia.ID ||
		got.OwnerID != visibleMedia.OwnerID ||
		got.StoragePath != visibleMedia.StoragePath ||
		!got.Visibility ||
		got.CreatedAt != visibleMedia.CreatedAt ||
		got.UpdatedAt != visibleMedia.UpdatedAt {
		t.Errorf("unexpected media payload: got %+v, want %+v", got, visibleMedia)
	}

	// 2. Soft-deleted media (Visibility == false) is immediately rejected with 404 NotFound
	deletedMedia := media_models.MediaPayload{
		ID:          102,
		OwnerID:     10,
		StoragePath: "s3://numan-media/users/10/deleted_102.webp",
		Visibility:  false,
		CreatedAt:   1700000003000,
		UpdatedAt:   1700000004000,
	}
	if err := SetMediaInObjectCache(ctx, deletedMedia); err != nil {
		t.Fatalf("SetMediaInObjectCache failed: %v", err)
	}

	_, err = GetMediaFromObjectCache(ctx, 102)
	if err == nil {
		t.Fatalf("expected error for soft-deleted media, got nil")
	}
	var appErr *numan_error.AppError
	if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusNotFound {
		t.Errorf("expected 404 NotFound AppError for soft-deleted media, got %v", err)
	}

	// 3. DeleteMediaFromObjectCache removes entry
	if err := DeleteMediaFromObjectCache(ctx, 101); err != nil {
		t.Fatalf("DeleteMediaFromObjectCache failed: %v", err)
	}
	if _, err := GetMediaFromObjectCache(ctx, 101); err == nil {
		t.Errorf("expected cache miss after DeleteMediaFromObjectCache, got nil")
	}

	// 4. Error branches
	srv.setFailCmd("SET", errors.New("set err"))
	if err := SetMediaInObjectCache(ctx, visibleMedia); err == nil {
		t.Errorf("expected error when SET fails, got nil")
	}
	srv.setFailCmd("SET", nil)

	srv.setFailCmd("DEL", errors.New("del err"))
	if err := DeleteMediaFromObjectCache(ctx, 101); err == nil {
		t.Errorf("expected error when DEL fails, got nil")
	}
}
