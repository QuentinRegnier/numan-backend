package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"math"
	"testing"
	"time"

	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/lib/pq"
	"github.com/vmihailenco/msgpack/v5"
)

func TestGraphCacheMarkovAndLazyPruning(t *testing.T) {
	ctx := context.Background()

	t.Run("UpdateTagCooccurrences ignores fewer than 2 tags", func(t *testing.T) {
		srv := setupTestRedis(t)
		UpdateTagCooccurrences(ctx, nil, 1000)
		UpdateTagCooccurrences(ctx, []string{"only_one"}, 1000)

		srv.mu.RLock()
		numHashes := len(srv.hashes)
		srv.mu.RUnlock()
		if numHashes != 0 {
			t.Errorf("expected 0 graph edges created for <2 tags, got %d", numHashes)
		}
	})

	t.Run("UpdateTagCooccurrences creates symmetric Markov edges and compounds weight on repeated co-occurrence", func(t *testing.T) {
		setupTestRedis(t)
		nowMs := time.Now().UnixMilli()

		UpdateTagCooccurrences(ctx, []string{"  GoLang ", "REDIS", ""}, nowMs)

		// Initial weight when W_old = 0: W_new = 0 + alpha
		wantInitial := variables.GraphLearningAlpha

		relatedToGo := GetRelatedTagsLazy(ctx, "golang")
		if got, ok := relatedToGo["redis"]; !ok || math.Abs(got-wantInitial) > 1e-4 {
			t.Fatalf("GetRelatedTagsLazy(\"golang\")[\"redis\"] = %v (ok=%v), want ~%v", got, ok, wantInitial)
		}

		relatedToRedis := GetRelatedTagsLazy(ctx, "  REDIS ")
		if got, ok := relatedToRedis["golang"]; !ok || math.Abs(got-wantInitial) > 1e-4 {
			t.Fatalf("GetRelatedTagsLazy(\"REDIS\")[\"golang\"] = %v (ok=%v), want ~%v", got, ok, wantInitial)
		}

		// Second co-occurrence at the same timestamp: W_new = W_old*(1-alpha) + alpha
		UpdateTagCooccurrences(ctx, []string{"golang", "redis"}, nowMs)
		wantSecond := wantInitial*(1.0-variables.GraphLearningAlpha) + variables.GraphLearningAlpha

		relatedAfterSecond := GetRelatedTagsLazy(ctx, "golang")
		if got := relatedAfterSecond["redis"]; math.Abs(got-wantSecond) > 1e-4 {
			t.Errorf("weight after second co-occurrence = %v, want ~%v", got, wantSecond)
		}
	})

	t.Run("GetRelatedTagsLazy prunes expired edges below GraphSurvivalEps asynchronously", func(t *testing.T) {
		setupTestRedis(t)
		nowMs := time.Now().UnixMilli()

		// Create one fresh edge and one ancient dead edge (1000 days old)
		freshEdge, _ := msgpack.Marshal(&tagEdge{Weight: 0.5, Timestamp: nowMs})
		ancientTs := nowMs - int64(1000*24*60*60*1000)
		deadEdge, _ := msgpack.Marshal(&tagEdge{Weight: 0.01, Timestamp: ancientTs})

		_ = repoRedis.GraphEdges.HSet(ctx, "ai",
			"ml", freshEdge,
			"obsolete", deadEdge,
			"corrupted", []byte("invalid_msgpack_bytes"),
		)

		surviving := GetRelatedTagsLazy(ctx, "ai")
		if _, ok := surviving["ml"]; !ok {
			t.Errorf("expected fresh edge 'ml' to survive, got %v", surviving)
		}
		if _, ok := surviving["obsolete"]; ok {
			t.Errorf("expected expired edge 'obsolete' to be filtered out, got %v", surviving)
		}

		// Wait briefly for async HDel goroutine to prune 'obsolete'
		time.Sleep(25 * time.Millisecond)
		if _, err := repoRedis.GraphEdges.HGet(ctx, "ai", "obsolete").Result(); err == nil {
			t.Errorf("expected 'obsolete' edge to be pruned from Redis hash by async HDel")
		}
	})

	t.Run("GetRelatedTagsLazy returns nil when source tag has no edges or Redis fails", func(t *testing.T) {
		srv := setupTestRedis(t)
		if got := GetRelatedTagsLazy(ctx, "nonexistent"); got != nil {
			t.Errorf("GetRelatedTagsLazy(\"nonexistent\") = %v, want nil", got)
		}

		srv.setFailCmd("HGETALL", errors.New("hgetall error"))
		defer srv.setFailCmd("HGETALL", nil)
		if got := GetRelatedTagsLazy(ctx, "ai"); got != nil {
			t.Errorf("GetRelatedTagsLazy() on error = %v, want nil", got)
		}
	})
}

func TestSeedGraphCache(t *testing.T) {
	ctx := context.Background()

	t.Run("replays historical posts from Postgres into GraphEdges", func(t *testing.T) {
		setupTestRedis(t)
		now := time.Now().UTC()

		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			h1, _ := pq.Array([]string{"docker", "kubernetes"}).Value()
			ind1, _ := pq.Array([]string{}).Value()
			return fakeSQLResponse{
				cols: []string{"id", "hashtags", "indirect_hashtags", "created_at"},
				rows: [][]driver.Value{
					{int64(1), h1, ind1, now},
				},
			}
		})

		if err := SeedGraphCache(ctx); err != nil {
			t.Fatalf("SeedGraphCache() unexpected error: %v", err)
		}

		related := GetRelatedTagsLazy(ctx, "docker")
		if _, ok := related["kubernetes"]; !ok {
			t.Errorf("expected SeedGraphCache to populate edge docker -> kubernetes, got %v", related)
		}
	})

	t.Run("returns internal error when Postgres fails", func(t *testing.T) {
		setupTestRedis(t)
		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			return fakeSQLResponse{err: errors.New("db error")}
		})

		if err := SeedGraphCache(ctx); err == nil {
			t.Fatal("SeedGraphCache() error = nil, want non-nil on Postgres failure")
		}
	})
}
