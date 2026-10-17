package cache_service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/sync_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

func TestMarkUserOnline(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("successfully marks user online with 90s TTL", func(t *testing.T) {
		err := MarkUserOnline(ctx, 101)
		if err != nil {
			t.Fatalf("MarkUserOnline(101) unexpected error: %v", err)
		}

		if !IsUserOnline(ctx, 101) {
			t.Fatalf("IsUserOnline(101) = false, want true after MarkUserOnline")
		}

		key := repoRedis.Presence.Key(101)
		if ttl := srv.getTTL(key); ttl != 90*time.Second {
			t.Errorf("Presence TTL for %s = %v, want %v", key, ttl, 90*time.Second)
		}
	})

	t.Run("returns internal error when SetPrimitive fails", func(t *testing.T) {
		srv.setFailCmd("SET", errors.New("redis set error"))
		defer srv.setFailCmd("SET", nil)

		err := MarkUserOnline(ctx, 102)
		if err == nil {
			t.Fatal("MarkUserOnline() error = nil, want non-nil error")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("MarkUserOnline() error = %v, want 500 AppError", err)
		}
	})

	t.Run("returns internal error when Expire fails", func(t *testing.T) {
		srv.setFailCmd("EXPIRE", errors.New("redis expire error"))
		defer srv.setFailCmd("EXPIRE", nil)

		err := MarkUserOnline(ctx, 103)
		if err == nil {
			t.Fatal("MarkUserOnline() error = nil, want non-nil error on Expire failure")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("MarkUserOnline() error = %v, want 500 AppError", err)
		}
	})
}

func TestIsUserOnline(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("returns false when user is not in cache", func(t *testing.T) {
		if got := IsUserOnline(ctx, 999); got {
			t.Fatalf("IsUserOnline(999) = true, want false")
		}
	})

	t.Run("returns true when user is marked online", func(t *testing.T) {
		_ = MarkUserOnline(ctx, 555)
		if got := IsUserOnline(ctx, 555); !got {
			t.Fatalf("IsUserOnline(555) = false, want true")
		}
	})

	t.Run("returns false when redis EXISTS fails", func(t *testing.T) {
		_ = MarkUserOnline(ctx, 556)
		srv.setFailCmd("EXISTS", errors.New("redis exists failure"))
		defer srv.setFailCmd("EXISTS", nil)

		if got := IsUserOnline(ctx, 556); got {
			t.Fatalf("IsUserOnline(556) on redis error = true, want false")
		}
	})
}

func TestAreUsersOnline(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("empty user IDs slice returns empty map without error", func(t *testing.T) {
		got, err := AreUsersOnline(ctx, sync_models.SyncPresenceInput{UserIDs: []int64{}})
		if err != nil {
			t.Fatalf("AreUsersOnline(empty) error = %v, want nil", err)
		}
		if len(got) != 0 {
			t.Fatalf("AreUsersOnline(empty) len = %d, want 0", len(got))
		}
	})

	t.Run("exceeding 100 user IDs returns forbidden error", func(t *testing.T) {
		tooMany := make([]int64, 101)
		for i := range tooMany {
			tooMany[i] = int64(i + 1)
		}
		_, err := AreUsersOnline(ctx, sync_models.SyncPresenceInput{UserIDs: tooMany})
		if err == nil {
			t.Fatal("AreUsersOnline(>100) error = nil, want Forbidden error")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusForbidden {
			t.Errorf("AreUsersOnline(>100) error = %v, want 403 Forbidden AppError", err)
		}
	})

	t.Run("mixed online and offline users returns accurate map", func(t *testing.T) {
		_ = MarkUserOnline(ctx, 10)
		_ = MarkUserOnline(ctx, 30)

		got, err := AreUsersOnline(ctx, sync_models.SyncPresenceInput{UserIDs: []int64{10, 20, 30, 40}})
		if err != nil {
			t.Fatalf("AreUsersOnline() unexpected error: %v", err)
		}

		expected := map[int64]bool{
			10: true,
			20: false,
			30: true,
			40: false,
		}
		for uid, want := range expected {
			if got[uid] != want {
				t.Errorf("AreUsersOnline()[%d] = %v, want %v", uid, got[uid], want)
			}
		}
	})

	t.Run("returns internal error when MGET fails", func(t *testing.T) {
		srv.setFailCmd("MGET", errors.New("redis mget error"))
		defer srv.setFailCmd("MGET", nil)

		_, err := AreUsersOnline(ctx, sync_models.SyncPresenceInput{UserIDs: []int64{10, 20}})
		if err == nil {
			t.Fatal("AreUsersOnline() on MGET failure error = nil, want non-nil")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("AreUsersOnline() error = %v, want 500 AppError", err)
		}
	})
}
