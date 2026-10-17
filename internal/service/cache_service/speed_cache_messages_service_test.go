package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

func TestGetMessageIDsFromSpeedCache(t *testing.T) {
	ctx := context.Background()

	t.Run("scrollDirection top and bottom with frozenMessageID and offsetMessageID on L1 hit", func(t *testing.T) {
		setupTestRedis(t)
		convID := int64(100)

		for _, msgID := range []int64{10, 20, 30, 40, 50} {
			_ = repoRedis.MessagesIndex.ZAdd(ctx, convID, float64(msgID), msgID)
		}

		// 1. scrollDirection = "top", no offset, no freeze, limit 3 -> [50, 40, 30]
		top3, err := GetMessageIDsFromSpeedCache(ctx, convID, 0, 3, "top", 0)
		if err != nil || !reflect.DeepEqual(top3, []int64{50, 40, 30}) {
			t.Errorf("top no-offset no-freeze = (%v, %v), want [50, 40, 30]", top3, err)
		}

		// 2. scrollDirection = "top", offsetMessageID = 40 (exclusive), limit 2 -> [30, 20]
		topOffset, err := GetMessageIDsFromSpeedCache(ctx, convID, 40, 2, "top", 0)
		if err != nil || !reflect.DeepEqual(topOffset, []int64{30, 20}) {
			t.Errorf("top offset=40 = (%v, %v), want [30, 20]", topOffset, err)
		}

		// 3. scrollDirection = "top", anti-cheat: offsetMessageID (50) > frozenMessageID (30) -> capped at 30 inclusive -> [30, 20]
		topFrozenCap, err := GetMessageIDsFromSpeedCache(ctx, convID, 50, 2, "top", 30)
		if err != nil || !reflect.DeepEqual(topFrozenCap, []int64{30, 20}) {
			t.Errorf("top offset=50 frozen=30 = (%v, %v), want [30, 20]", topFrozenCap, err)
		}

		// 4. scrollDirection = "bottom", offsetMessageID = 10 (exclusive), frozenMessageID = 40, limit 3 -> [20, 30, 40]
		bottomRange, err := GetMessageIDsFromSpeedCache(ctx, convID, 10, 3, "bottom", 40)
		if err != nil || !reflect.DeepEqual(bottomRange, []int64{20, 30, 40}) {
			t.Errorf("bottom offset=10 frozen=40 = (%v, %v), want [20, 30, 40]", bottomRange, err)
		}
	})

	t.Run("cache hole (len < limit) or Redis error triggers L3 Postgres fallback and async L1 rehydration", func(t *testing.T) {
		setupTestRedis(t)
		convID := int64(200)

		// Only 1 message in L1, but paginationLimit is 3 -> triggers L3 fallback
		_ = repoRedis.MessagesIndex.ZAdd(ctx, convID, 100, "100")

		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			return fakeSQLResponse{
				cols: []string{"id"},
				rows: [][]driver.Value{
					{int64(300)},
					{int64(200)},
					{int64(100)},
				},
			}
		})

		got, err := GetMessageIDsFromSpeedCache(ctx, convID, 0, 3, "top", 0)
		if err != nil {
			t.Fatalf("GetMessageIDsFromSpeedCache(fallback) error: %v", err)
		}
		want := []int64{300, 200, 100}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("GetMessageIDsFromSpeedCache(fallback) = %v, want %v", got, want)
		}

		// Wait for async L1 rehydration goroutine
		time.Sleep(25 * time.Millisecond)
		rehydrated, _ := repoRedis.MessagesIndex.ZRevRange(ctx, convID, 0, -1)
		if !reflect.DeepEqual(rehydrated, []string{"300", "200", "100"}) {
			t.Errorf("expected L1 MessagesIndex to be rehydrated with [300, 200, 100], got %v", rehydrated)
		}
	})

	t.Run("returns 500 error when both L1 and L3 fail", func(t *testing.T) {
		srv := setupTestRedis(t)
		srv.setFailCmd("ZREVRANGEBYSCORE", errors.New("redis down"))
		defer srv.setFailCmd("ZREVRANGEBYSCORE", nil)

		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			return fakeSQLResponse{err: errors.New("postgres down")}
		})

		_, err := GetMessageIDsFromSpeedCache(ctx, 300, 0, 10, "top", 0)
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("GetMessageIDsFromSpeedCache() error = %v, want 500 AppError", err)
		}
	})
}
