package cache_service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

func TestSetWatermarkInSpeedCache(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("stores watermark in hash and refreshes TTL", func(t *testing.T) {
		err := SetWatermarkInSpeedCache(ctx, 500, 42, 9001)
		if err != nil {
			t.Fatalf("SetWatermarkInSpeedCache() unexpected error: %v", err)
		}

		watermarks, err := GetWatermarksFromSpeedCache(ctx, 500)
		if err != nil {
			t.Fatalf("GetWatermarksFromSpeedCache() unexpected error: %v", err)
		}
		if got := watermarks[42]; got != 9001 {
			t.Errorf("watermark[42] = %d, want 9001", got)
		}

		key := repoRedis.ConvWatermarks.Key(500)
		if ttl := srv.getTTL(key); ttl != variables.StandardTTL {
			t.Errorf("TTL for %s = %v, want %v", key, ttl, variables.StandardTTL)
		}
	})

	t.Run("overwrites existing watermark for same user", func(t *testing.T) {
		_ = SetWatermarkInSpeedCache(ctx, 501, 42, 100)
		if err := SetWatermarkInSpeedCache(ctx, 501, 42, 250); err != nil {
			t.Fatalf("SetWatermarkInSpeedCache() update error: %v", err)
		}

		watermarks, err := GetWatermarksFromSpeedCache(ctx, 501)
		if err != nil {
			t.Fatalf("GetWatermarksFromSpeedCache() error: %v", err)
		}
		if got := watermarks[42]; got != 250 {
			t.Errorf("watermark[42] = %d, want 250", got)
		}
	})

	t.Run("returns internal error when HSET fails", func(t *testing.T) {
		srv.setFailCmd("HSET", errors.New("hset failure"))
		defer srv.setFailCmd("HSET", nil)

		err := SetWatermarkInSpeedCache(ctx, 502, 42, 100)
		if err == nil {
			t.Fatal("SetWatermarkInSpeedCache() error = nil, want non-nil")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("SetWatermarkInSpeedCache() error = %v, want 500 AppError", err)
		}
	})
}

func TestGetWatermarksFromSpeedCache(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("empty conversation returns empty map and does not refresh TTL", func(t *testing.T) {
		got, err := GetWatermarksFromSpeedCache(ctx, 777)
		if err != nil {
			t.Fatalf("GetWatermarksFromSpeedCache(777) unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("GetWatermarksFromSpeedCache(777) len = %d, want 0", len(got))
		}
		key := repoRedis.ConvWatermarks.Key(777)
		if ttl := srv.getTTL(key); ttl != 0 {
			t.Errorf("expected TTL 0 for empty watermark hash, got %v", ttl)
		}
	})

	t.Run("parses valid entries and skips malformed user or message IDs", func(t *testing.T) {
		_ = repoRedis.ConvWatermarks.HSet(ctx, 888,
			"10", "1000",
			"20", "2000",
			"invalid_user", "3000",
			"30", "invalid_msg",
		)
		// Clear TTL before calling GetWatermarksFromSpeedCache to verify it refreshes TTL
		srv.mu.Lock()
		delete(srv.ttls, repoRedis.ConvWatermarks.Key(888))
		srv.mu.Unlock()

		got, err := GetWatermarksFromSpeedCache(ctx, 888)
		if err != nil {
			t.Fatalf("GetWatermarksFromSpeedCache(888) unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("GetWatermarksFromSpeedCache(888) len = %d, want 2 (got %v)", len(got), got)
		}
		if got[10] != 1000 || got[20] != 2000 {
			t.Errorf("GetWatermarksFromSpeedCache(888) = %v, want map[10:1000 20:2000]", got)
		}
		if ttl := srv.getTTL(repoRedis.ConvWatermarks.Key(888)); ttl != time.Duration(variables.StandardTTL) {
			t.Errorf("TTL after non-empty GetWatermarksFromSpeedCache = %v, want %v", ttl, variables.StandardTTL)
		}
	})

	t.Run("returns internal error when HGETALL fails", func(t *testing.T) {
		srv.setFailCmd("HGETALL", errors.New("hgetall failure"))
		defer srv.setFailCmd("HGETALL", nil)

		_, err := GetWatermarksFromSpeedCache(ctx, 888)
		if err == nil {
			t.Fatal("GetWatermarksFromSpeedCache() error = nil, want non-nil")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("GetWatermarksFromSpeedCache() error = %v, want 500 AppError", err)
		}
	})
}
