package cache_service

import (
	"context"
	"errors"
	"testing"

	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

func TestGetIdempotencyCollection(t *testing.T) {
	setupTestRedis(t)

	t.Run("returns PostLikesSet for LikeTargetTypePost", func(t *testing.T) {
		got := getIdempotencyCollection(variables.LikeTargetTypePost)
		if got != repoRedis.PostLikesSet {
			t.Fatalf("getIdempotencyCollection(%d) = %v, want %v", variables.LikeTargetTypePost, got, repoRedis.PostLikesSet)
		}
	})

	t.Run("returns CommentLikesSet for LikeTargetTypeComment", func(t *testing.T) {
		got := getIdempotencyCollection(variables.LikeTargetTypeComment)
		if got != repoRedis.CommentLikesSet {
			t.Fatalf("getIdempotencyCollection(%d) = %v, want %v", variables.LikeTargetTypeComment, got, repoRedis.CommentLikesSet)
		}
	})

	t.Run("returns nil for unknown target entity types", func(t *testing.T) {
		for _, invalidType := range []int{-1, 2, 99} {
			if got := getIdempotencyCollection(invalidType); got != nil {
				t.Errorf("getIdempotencyCollection(%d) = %v, want nil", invalidType, got)
			}
		}
	})
}

func TestTryAddLikeIdempotency(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("first like on post returns true and duplicate returns false", func(t *testing.T) {
		if got := TryAddLikeIdempotency(ctx, variables.LikeTargetTypePost, 1001, 42); !got {
			t.Fatalf("TryAddLikeIdempotency(post=1001, user=42) first call = false, want true")
		}
		if got := TryAddLikeIdempotency(ctx, variables.LikeTargetTypePost, 1001, 42); got {
			t.Fatalf("TryAddLikeIdempotency(post=1001, user=42) duplicate call = true, want false")
		}
	})

	t.Run("first like on comment returns true and duplicate returns false", func(t *testing.T) {
		if got := TryAddLikeIdempotency(ctx, variables.LikeTargetTypeComment, 2002, 42); !got {
			t.Fatalf("TryAddLikeIdempotency(comment=2002, user=42) first call = false, want true")
		}
		if got := TryAddLikeIdempotency(ctx, variables.LikeTargetTypeComment, 2002, 42); got {
			t.Fatalf("TryAddLikeIdempotency(comment=2002, user=42) duplicate call = true, want false")
		}
	})

	t.Run("returns false when redis SADD fails", func(t *testing.T) {
		srv.setFailCmd("SADD", errors.New("redis connection failure"))
		defer srv.setFailCmd("SADD", nil)

		if got := TryAddLikeIdempotency(ctx, variables.LikeTargetTypePost, 3003, 99); got {
			t.Fatalf("TryAddLikeIdempotency() on redis error = true, want false")
		}
	})
}

func TestTryRemoveLikeIdempotency(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("removing existing like returns true and second removal returns false", func(t *testing.T) {
		_ = TryAddLikeIdempotency(ctx, variables.LikeTargetTypePost, 500, 10)

		if got := TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypePost, 500, 10); !got {
			t.Fatalf("TryRemoveLikeIdempotency(post=500, user=10) = false, want true")
		}
		if got := TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypePost, 500, 10); got {
			t.Fatalf("TryRemoveLikeIdempotency(post=500, user=10) second call = true, want false")
		}
	})

	t.Run("removing non-existent comment like returns false", func(t *testing.T) {
		if got := TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypeComment, 600, 10); got {
			t.Fatalf("TryRemoveLikeIdempotency(comment=600, user=10) = true, want false")
		}
	})

	t.Run("returns false when redis SREM fails", func(t *testing.T) {
		_ = TryAddLikeIdempotency(ctx, variables.LikeTargetTypeComment, 700, 10)
		srv.setFailCmd("SREM", errors.New("redis srem failure"))
		defer srv.setFailCmd("SREM", nil)

		if got := TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypeComment, 700, 10); got {
			t.Fatalf("TryRemoveLikeIdempotency() on redis error = true, want false")
		}
	})
}
