package object_cache_service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
)

func TestMediaObjectCache_LifecycleAndSoftDeleteCheck(t *testing.T) {
	srv := test.SetupTestRedis(t)
	ctx := context.Background()

	// Cache miss returns 404 CodeNotFound AppError
	_, errMiss := GetMediaFromObjectCache(ctx, 999)
	assertAppError(t, errMiss, http.StatusNotFound, numan_error.CodeNotFound)

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
	assertAppError(t, err, http.StatusNotFound, numan_error.CodeNotFound)

	// 3. DeleteMediaFromObjectCache removes entry
	if err := DeleteMediaFromObjectCache(ctx, 101); err != nil {
		t.Fatalf("DeleteMediaFromObjectCache failed: %v", err)
	}
	_, errAfterDel := GetMediaFromObjectCache(ctx, 101)
	assertAppError(t, errAfterDel, http.StatusNotFound, numan_error.CodeNotFound)

	// 4. Error branches
	srv.SetFailCmd("GET", errors.New("get err"))
	_, errGet := GetMediaFromObjectCache(ctx, 101)
	assertAppError(t, errGet, http.StatusInternalServerError, numan_error.CodeCacheError)
	srv.SetFailCmd("GET", nil)

	srv.SetFailCmd("SET", errors.New("set err"))
	assertAppError(t, SetMediaInObjectCache(ctx, visibleMedia), http.StatusInternalServerError, numan_error.CodeCacheError)
	srv.SetFailCmd("SET", nil)

	srv.SetFailCmd("DEL", errors.New("del err"))
	assertAppError(t, DeleteMediaFromObjectCache(ctx, 101), http.StatusInternalServerError, numan_error.CodeCacheError)
}
