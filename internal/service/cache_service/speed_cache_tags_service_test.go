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

func TestStoreTagInSpeedCacheAndSearchTagsByPrefix(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("stores tags normalized to lowercase and searches by prefix with pagination", func(t *testing.T) {
		tags := []string{"Golang", "Goopher", "GoRoutine", "Python"}
		for _, tag := range tags {
			if err := StoreTagInSpeedCache(ctx, tag); err != nil {
				t.Fatalf("StoreTagInSpeedCache(%q) unexpected error: %v", tag, err)
			}
		}

		// Search prefix "go" (case-insensitive)
		got, err := SearchTagsByPrefix(ctx, "GO", 0, 10)
		if err != nil {
			t.Fatalf("SearchTagsByPrefix(\"GO\") unexpected error: %v", err)
		}
		want := []string{"golang", "goopher", "goroutine"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("SearchTagsByPrefix(\"GO\", 0, 10) = %v, want %v", got, want)
		}

		// Paginated: offset 1, limit 1
		gotPage, err := SearchTagsByPrefix(ctx, "go", 1, 1)
		if err != nil {
			t.Fatalf("SearchTagsByPrefix(\"go\", 1, 1) unexpected error: %v", err)
		}
		wantPage := []string{"goopher"}
		if !reflect.DeepEqual(gotPage, wantPage) {
			t.Errorf("SearchTagsByPrefix(\"go\", 1, 1) = %v, want %v", gotPage, wantPage)
		}
	})

	t.Run("returns empty non-nil slice when no tags match or prefix is empty", func(t *testing.T) {
		gotNoMatch, err := SearchTagsByPrefix(ctx, "nonexistent", 0, 10)
		if err != nil {
			t.Fatalf("SearchTagsByPrefix(\"nonexistent\") error = %v, want nil", err)
		}
		if gotNoMatch == nil || len(gotNoMatch) != 0 {
			t.Errorf("SearchTagsByPrefix(\"nonexistent\") = %v, want empty non-nil slice", gotNoMatch)
		}

		gotEmptyPrefix, err := SearchTagsByPrefix(ctx, "", 0, 10)
		if err != nil {
			t.Fatalf("SearchTagsByPrefix(\"\") error = %v, want nil", err)
		}
		if gotEmptyPrefix == nil || len(gotEmptyPrefix) != 0 {
			t.Errorf("SearchTagsByPrefix(\"\") = %v, want empty non-nil slice", gotEmptyPrefix)
		}
	})

	t.Run("SearchTagsByPrefix returns internal error on ZRANGEBYLEX failure", func(t *testing.T) {
		srv.setFailCmd("ZRANGEBYLEX", errors.New("zrangebylex failure"))
		defer srv.setFailCmd("ZRANGEBYLEX", nil)

		_, err := SearchTagsByPrefix(ctx, "go", 0, 10)
		if err == nil {
			t.Fatal("SearchTagsByPrefix() error = nil, want non-nil")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("SearchTagsByPrefix() error = %v, want 500 AppError", err)
		}
	})

	t.Run("StoreTagInSpeedCache returns internal error on ZADD failure", func(t *testing.T) {
		srv.setFailCmd("ZADD", errors.New("zadd failure"))
		defer srv.setFailCmd("ZADD", nil)

		err := StoreTagInSpeedCache(ctx, "newtag")
		if err == nil {
			t.Fatal("StoreTagInSpeedCache() error = nil, want non-nil")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("StoreTagInSpeedCache() error = %v, want 500 AppError", err)
		}
	})
}

func TestIsTagTrending(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("returns true when tag is in HashtagLeaderboard and false otherwise", func(t *testing.T) {
		_ = repoRedis.HashtagLeaderboard.ZAddWithCap(ctx, "global", 42.5, "trending_tag", variables.TDDMaxZSET)

		if !IsTagTrending(ctx, "trending_tag") {
			t.Errorf("IsTagTrending(\"trending_tag\") = false, want true")
		}
		if IsTagTrending(ctx, "unknown_tag") {
			t.Errorf("IsTagTrending(\"unknown_tag\") = true, want false")
		}
	})

	t.Run("returns false when ZSCORE fails", func(t *testing.T) {
		srv.setFailCmd("ZSCORE", errors.New("zscore error"))
		defer srv.setFailCmd("ZSCORE", nil)

		if IsTagTrending(ctx, "trending_tag") {
			t.Errorf("IsTagTrending() on ZSCORE error = true, want false")
		}
	})
}
