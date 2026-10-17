package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

func TestGetAddableUsersFromSpeedCache(t *testing.T) {
	ctx := context.Background()

	t.Run("cache miss reconstructs ZSET with Friends ordered before Followers and lexicographically by username", func(t *testing.T) {
		srv := setupTestRedis(t)
		callerID := int64(1)

		// Pre-populate UsersLite in L1 for target users:
		// Target 10: follower (state 1), username "alice" -> "1_alice_10"
		// Target 20: friend (state 2), username "zoe" -> "0_zoe_20"
		// Target 30: friend (state 2), username "bob" -> "0_bob_30"
		u10 := lite_models.UserLiteRequest{ID: 10, Username: "Alice"}
		u20 := lite_models.UserLiteRequest{ID: 20, Username: "Zoe"}
		u30 := lite_models.UserLiteRequest{ID: 30, Username: "Bob"}
		_ = repoRedis.UsersLite.SetObject(ctx, int64(10), u10)
		_ = repoRedis.UsersLite.SetObject(ctx, int64(20), u20)
		_ = repoRedis.UsersLite.SetObject(ctx, int64(30), u30)

		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			return fakeSQLResponse{
				cols: []string{"target_id", "state"},
				rows: [][]driver.Value{
					{int64(10), int64(variables.RelationStateFollow)},
					{int64(20), int64(variables.RelationStateFriend)},
					{int64(30), int64(variables.RelationStateFriend)},
					{int64(99), int64(variables.RelationStateFriend)}, // missing in UsersLite -> skipped
				},
			}
		})

		got, err := GetAddableUsersFromSpeedCache(ctx, callerID, 10, 0, false)
		if err != nil {
			t.Fatalf("GetAddableUsersFromSpeedCache() unexpected error: %v", err)
		}

		// Expected order: Bob (0_bob_30), Zoe (0_zoe_20), Alice (1_alice_10)
		wantIDs := []int64{30, 20, 10}
		gotIDs := []int64{got[0].ID, got[1].ID, got[2].ID}
		if !reflect.DeepEqual(gotIDs, wantIDs) {
			t.Errorf("GetAddableUsersFromSpeedCache() order = %v, want %v", gotIDs, wantIDs)
		}

		zsetKey := repoRedis.SpeedAddable.Key(callerID)
		if ttl := srv.getTTL(zsetKey); ttl != 5*time.Minute {
			t.Errorf("SpeedAddable TTL = %v, want 5m", ttl)
		}

		// Subsequent call with forceRefresh=false hits L1 without calling Postgres
		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			return fakeSQLResponse{err: errors.New("should not query postgres on L1 hit")}
		})
		gotCached, err := GetAddableUsersFromSpeedCache(ctx, callerID, 2, 1, false)
		if err != nil {
			t.Fatalf("GetAddableUsersFromSpeedCache(cached) error: %v", err)
		}
		if len(gotCached) != 2 || gotCached[0].ID != 20 || gotCached[1].ID != 10 {
			t.Errorf("GetAddableUsersFromSpeedCache(cached page) = %+v, want IDs [20, 10]", gotCached)
		}
	})

	t.Run("empty relations list injects SpeedCacheEmptyMarker and returns empty slice", func(t *testing.T) {
		setupTestRedis(t)
		callerID := int64(2)

		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			return fakeSQLResponse{
				cols: []string{"target_id", "state"},
				rows: nil,
			}
		})

		got, err := GetAddableUsersFromSpeedCache(ctx, callerID, 10, 0, true)
		if err != nil {
			t.Fatalf("GetAddableUsersFromSpeedCache(empty) error: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("GetAddableUsersFromSpeedCache(empty) = %+v, want empty slice", got)
		}

		// Verify empty marker is present in ZSET
		members, _ := repoRedis.ZRange(ctx, repoRedis.SpeedAddable.Key(callerID), 0, -1)
		if !reflect.DeepEqual(members, []string{variables.SpeedCacheEmptyMarker}) {
			t.Errorf("ZSET members = %v, want [%q]", members, variables.SpeedCacheEmptyMarker)
		}
	})

	t.Run("error propagation on Redis and Postgres failures", func(t *testing.T) {
		srv := setupTestRedis(t)

		srv.setFailCmd("EXISTS", errors.New("exists error"))
		_, err := GetAddableUsersFromSpeedCache(ctx, 3, 10, 0, false)
		srv.setFailCmd("EXISTS", nil)
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("expected 500 AppError on EXISTS failure, got %v", err)
		}

		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			return fakeSQLResponse{err: errors.New("pg failure")}
		})
		_, err = GetAddableUsersFromSpeedCache(ctx, 3, 10, 0, false)
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("expected 500 AppError on Postgres failure, got %v", err)
		}
	})
}
