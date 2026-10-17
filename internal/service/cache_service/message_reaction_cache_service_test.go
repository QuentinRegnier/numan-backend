package cache_service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

func TestUserReactionLifecycle(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("GetUserReaction returns empty string and nil error on cache miss", func(t *testing.T) {
		emoji, err := GetUserReaction(ctx, 100, 42)
		if err != nil {
			t.Fatalf("GetUserReaction() error = %v, want nil", err)
		}
		if emoji != "" {
			t.Fatalf("GetUserReaction() = %q, want empty string", emoji)
		}
	})

	t.Run("SetUserReaction stores emoji and GetUserReaction retrieves it", func(t *testing.T) {
		if err := SetUserReaction(ctx, 100, 42, "🔥"); err != nil {
			t.Fatalf("SetUserReaction() unexpected error: %v", err)
		}

		emoji, err := GetUserReaction(ctx, 100, 42)
		if err != nil {
			t.Fatalf("GetUserReaction() unexpected error: %v", err)
		}
		if emoji != "🔥" {
			t.Fatalf("GetUserReaction() = %q, want %q", emoji, "🔥")
		}
	})

	t.Run("DeleteUserReaction removes emoji from cache", func(t *testing.T) {
		_ = SetUserReaction(ctx, 100, 42, "❤️")
		if err := DeleteUserReaction(ctx, 100, 42); err != nil {
			t.Fatalf("DeleteUserReaction() unexpected error: %v", err)
		}

		emoji, err := GetUserReaction(ctx, 100, 42)
		if err != nil || emoji != "" {
			t.Fatalf("GetUserReaction() after delete = (%q, %v), want (\"\", nil)", emoji, err)
		}
	})

	t.Run("SetUserReaction returns internal error on HSET failure", func(t *testing.T) {
		srv.setFailCmd("HSET", errors.New("redis hset error"))
		defer srv.setFailCmd("HSET", nil)

		err := SetUserReaction(ctx, 100, 42, "👍")
		if err == nil {
			t.Fatal("SetUserReaction() error = nil, want non-nil")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("SetUserReaction() error = %v, want 500 AppError", err)
		}
	})

	t.Run("DeleteUserReaction returns internal error on HDEL failure", func(t *testing.T) {
		srv.setFailCmd("HDEL", errors.New("redis hdel error"))
		defer srv.setFailCmd("HDEL", nil)

		err := DeleteUserReaction(ctx, 100, 42)
		if err == nil {
			t.Fatal("DeleteUserReaction() error = nil, want non-nil")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("DeleteUserReaction() error = %v, want 500 AppError", err)
		}
	})
}

func TestReactionCounts(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("IncrementReactionCount increments and auto-deletes when count reaches zero or negative", func(t *testing.T) {
		msgID := int64(500)

		if err := IncrementReactionCount(ctx, msgID, "👍", 2); err != nil {
			t.Fatalf("IncrementReactionCount(+2) error: %v", err)
		}
		if err := IncrementReactionCount(ctx, msgID, "🎉", 1); err != nil {
			t.Fatalf("IncrementReactionCount(+1) error: %v", err)
		}

		counts, err := GetMessageReactionCounts(ctx, msgID)
		if err != nil {
			t.Fatalf("GetMessageReactionCounts() error: %v", err)
		}
		if counts["👍"] != 2 || counts["🎉"] != 1 {
			t.Fatalf("GetMessageReactionCounts() = %v, want map[👍:2 🎉:1]", counts)
		}

		// Decrement 🎉 to 0 -> should be removed from hash
		if err := IncrementReactionCount(ctx, msgID, "🎉", -1); err != nil {
			t.Fatalf("IncrementReactionCount(-1) error: %v", err)
		}
		// Decrement 👍 by -5 -> negative -> should be removed from hash
		if err := IncrementReactionCount(ctx, msgID, "👍", -5); err != nil {
			t.Fatalf("IncrementReactionCount(-5) error: %v", err)
		}

		countsAfter, err := GetMessageReactionCounts(ctx, msgID)
		if err != nil {
			t.Fatalf("GetMessageReactionCounts() error: %v", err)
		}
		if len(countsAfter) != 0 {
			t.Fatalf("GetMessageReactionCounts() after zero/negative = %v, want empty map", countsAfter)
		}
	})

	t.Run("GetMessageReactionCounts filters out non-positive or malformed counts", func(t *testing.T) {
		msgID := int64(501)
		_ = repoRedis.MessageReactionCounts.HSet(ctx, msgID,
			"🔥", "5",
			"❄️", "0",
			"💀", "-2",
			"🐛", "not_a_number",
		)

		counts, err := GetMessageReactionCounts(ctx, msgID)
		if err != nil {
			t.Fatalf("GetMessageReactionCounts() error: %v", err)
		}
		if len(counts) != 1 || counts["🔥"] != 5 {
			t.Fatalf("GetMessageReactionCounts() = %v, want map[🔥:5]", counts)
		}
	})

	t.Run("IncrementReactionCount returns internal error on HINCRBY failure", func(t *testing.T) {
		srv.setFailCmd("HINCRBY", errors.New("hincrby failure"))
		defer srv.setFailCmd("HINCRBY", nil)

		err := IncrementReactionCount(ctx, 502, "👍", 1)
		if err == nil {
			t.Fatal("IncrementReactionCount() error = nil, want non-nil")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("IncrementReactionCount() error = %v, want 500 AppError", err)
		}
	})

	t.Run("GetMessageReactionCounts returns internal error on HGETALL failure", func(t *testing.T) {
		srv.setFailCmd("HGETALL", errors.New("hgetall failure"))
		defer srv.setFailCmd("HGETALL", nil)

		_, err := GetMessageReactionCounts(ctx, 502)
		if err == nil {
			t.Fatal("GetMessageReactionCounts() error = nil, want non-nil")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("GetMessageReactionCounts() error = %v, want 500 AppError", err)
		}
	})
}
