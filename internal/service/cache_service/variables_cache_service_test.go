package cache_service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

func TestVariablesCacheReadinessAndHooks(t *testing.T) {
	setupTestRedis(t)

	t.Run("IsVariablesCacheReady reflects infraRedis.Rdb non-nil state", func(t *testing.T) {
		if !IsVariablesCacheReady() {
			t.Fatal("IsVariablesCacheReady() = false, want true when Rdb is initialized")
		}

		saved := infraRedis.Rdb
		infraRedis.Rdb = nil
		if IsVariablesCacheReady() {
			infraRedis.Rdb = saved
			t.Fatal("IsVariablesCacheReady() = true, want false when Rdb is nil")
		}
		infraRedis.Rdb = saved
	})

	t.Run("BindVariablesCacheHooks binds RefreshStandardTTL to variables registry", func(t *testing.T) {
		BindVariablesCacheHooks()
		defer func() {
			_ = variables.ApplyRemoteState(map[string]string{}, 0)
		}()

		err := variables.ApplyRemoteState(map[string]string{
			"StandardTTL": `"45m0s"`,
		}, 1)
		if err != nil {
			t.Fatalf("ApplyRemoteState(StandardTTL) unexpected error: %v", err)
		}

		if repoRedis.Posts.DefaultTTL != 45*time.Minute {
			t.Errorf("repoRedis.Posts.DefaultTTL = %v, want %v", repoRedis.Posts.DefaultTTL, 45*time.Minute)
		}
	})
}

func TestDynamicVariablesStateAndPatch(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("GetDynamicVariablesRemoteVersion returns 0 on redis.Nil and actual version after patch", func(t *testing.T) {
		ver0, err := GetDynamicVariablesRemoteVersion(ctx)
		if err != nil {
			t.Fatalf("GetDynamicVariablesRemoteVersion() initial error: %v", err)
		}
		if ver0 != 0 {
			t.Errorf("GetDynamicVariablesRemoteVersion() initial = %d, want 0", ver0)
		}

		newVer, err := SaveDynamicVariablesPatchInCache(ctx, map[string]string{
			"FeedPageSize": "100",
			"SocialRatio":  "0.5",
		}, nil)
		if err != nil {
			t.Fatalf("SaveDynamicVariablesPatchInCache() error: %v", err)
		}
		if newVer != 1 {
			t.Errorf("SaveDynamicVariablesPatchInCache() version = %d, want 1", newVer)
		}

		ver1, err := GetDynamicVariablesRemoteVersion(ctx)
		if err != nil || ver1 != 1 {
			t.Errorf("GetDynamicVariablesRemoteVersion() = (%d, %v), want (1, nil)", ver1, err)
		}

		// Delete SocialRatio and update FeedPageSize
		newVer2, err := SaveDynamicVariablesPatchInCache(ctx, map[string]string{
			"FeedPageSize": "150",
		}, []string{"SocialRatio"})
		if err != nil || newVer2 != 2 {
			t.Fatalf("SaveDynamicVariablesPatchInCache(v2) = (%d, %v), want (2, nil)", newVer2, err)
		}

		overrides, loadedVer, err := LoadDynamicVariablesFromCache(ctx)
		if err != nil {
			t.Fatalf("LoadDynamicVariablesFromCache() error: %v", err)
		}
		if loadedVer != 2 {
			t.Errorf("LoadDynamicVariablesFromCache() version = %d, want 2", loadedVer)
		}
		if overrides["FeedPageSize"] != "150" {
			t.Errorf("overrides[\"FeedPageSize\"] = %q, want \"150\"", overrides["FeedPageSize"])
		}
		if _, exists := overrides["SocialRatio"]; exists {
			t.Errorf("expected SocialRatio to be deleted from overrides, got %v", overrides)
		}
	})

	t.Run("PublishDynamicVariablesSync and SubscribeDynamicVariablesSync deliver event via Claim-Check Flux", func(t *testing.T) {
		ch, cancel := SubscribeDynamicVariablesSync(ctx)
		defer cancel()

		// Give subscriber goroutine a brief moment to register SUBSCRIBE command
		time.Sleep(30 * time.Millisecond)

		evt := VariablesSyncEvent{Version: 7, UpdatedAt: 1700000000000}
		if err := PublishDynamicVariablesSync(ctx, evt); err != nil {
			t.Fatalf("PublishDynamicVariablesSync() error: %v", err)
		}

		select {
		case raw := <-ch:
			var got VariablesSyncEvent
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("failed to unmarshal VariablesSyncEvent: %v", err)
			}
			if got != evt {
				t.Errorf("received VariablesSyncEvent = %+v, want %+v", got, evt)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("timed out waiting for VariablesSyncEvent from SubscribeDynamicVariablesSync")
		}
	})

	t.Run("ApplyAndBroadcastDynamicVariablesPatch persists patch and tolerates pubsub failure", func(t *testing.T) {
		patch := &variables.PreparedPatch{
			ToSet:    map[string]string{"MaxPinConversation": "5"},
			ToDelete: []string{"FeedPageSize"},
		}

		ver, err := ApplyAndBroadcastDynamicVariablesPatch(ctx, patch)
		if err != nil {
			t.Fatalf("ApplyAndBroadcastDynamicVariablesPatch() error: %v", err)
		}
		if ver != 3 {
			t.Errorf("ApplyAndBroadcastDynamicVariablesPatch() version = %d, want 3", ver)
		}

		// Even if PUBLISH fails, ApplyAndBroadcastDynamicVariablesPatch should still succeed
		srv.setFailCmd("PUBLISH", errors.New("pubsub down"))
		ver4, err := ApplyAndBroadcastDynamicVariablesPatch(ctx, patch)
		srv.setFailCmd("PUBLISH", nil)
		if err != nil || ver4 != 4 {
			t.Errorf("ApplyAndBroadcastDynamicVariablesPatch() with PUBLISH error = (%d, %v), want (4, nil)", ver4, err)
		}
	})

	t.Run("ApplyAndBroadcastDynamicVariablesPatch fails when Redis is not ready or EVAL fails", func(t *testing.T) {
		patch := &variables.PreparedPatch{ToSet: map[string]string{"MaxPinConversation": "5"}}

		saved := infraRedis.Rdb
		infraRedis.Rdb = nil
		_, err := ApplyAndBroadcastDynamicVariablesPatch(ctx, patch)
		infraRedis.Rdb = saved
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("ApplyAndBroadcastDynamicVariablesPatch() when Redis not ready = %v, want 500 AppError", err)
		}

		srv.setFailCmd("EVAL", errors.New("lua eval failure"))
		_, err = ApplyAndBroadcastDynamicVariablesPatch(ctx, patch)
		srv.setFailCmd("EVAL", nil)
		if err == nil {
			t.Fatal("ApplyAndBroadcastDynamicVariablesPatch() expected error when EVAL fails")
		}
	})
}
