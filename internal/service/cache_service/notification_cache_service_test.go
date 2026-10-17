package cache_service

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

func TestAddAndGetNotificationsZSET(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("adds notifications, caps at 100, and returns paginated descending IDs while refreshing TTL", func(t *testing.T) {
		userID := int64(42)

		// Insert 105 notifications to verify 100-item Lua cap
		for i := int64(1); i <= 105; i++ {
			if err := AddNotificationToZSET(ctx, userID, 1000+i, i*100); err != nil {
				t.Fatalf("AddNotificationToZSET(%d) unexpected error: %v", i, err)
			}
		}

		// Top 3 should be 1105, 1104, 1103
		top3, err := GetNotificationIDsFromZSET(ctx, userID, 0, 3)
		if err != nil {
			t.Fatalf("GetNotificationIDsFromZSET() error: %v", err)
		}
		wantTop3 := []int64{1105, 1104, 1103}
		if !reflect.DeepEqual(top3, wantTop3) {
			t.Errorf("GetNotificationIDsFromZSET(0, 3) = %v, want %v", top3, wantTop3)
		}

		// Total items in ZSET should be capped at 100
		allIDs, err := GetNotificationIDsFromZSET(ctx, userID, 0, 200)
		if err != nil {
			t.Fatalf("GetNotificationIDsFromZSET(0, 200) error: %v", err)
		}
		if len(allIDs) != 100 {
			t.Errorf("ZSET size after 105 inserts = %d, want 100", len(allIDs))
		}
		// Lowest surviving item should be 1006 (since 1001..1005 were evicted)
		if allIDs[len(allIDs)-1] != 1006 {
			t.Errorf("oldest surviving notification ID = %d, want 1006", allIDs[len(allIDs)-1])
		}

		if ttl := srv.getTTL(repoRedis.NotificationsZSet.Key(userID)); ttl != variables.StandardTTL {
			t.Errorf("NotificationsZSet TTL = %v, want %v", ttl, variables.StandardTTL)
		}
	})

	t.Run("AddNotificationToZSET returns error when EVAL fails", func(t *testing.T) {
		srv.setFailCmd("EVAL", errors.New("eval script error"))
		defer srv.setFailCmd("EVAL", nil)

		if err := AddNotificationToZSET(ctx, 42, 9999, 12345); err == nil {
			t.Fatal("AddNotificationToZSET() error = nil, want non-nil on EVAL failure")
		}
	})

	t.Run("AddNotificationToZSET returns error when RefreshTTL fails", func(t *testing.T) {
		srv.setFailCmd("EXPIRE", errors.New("expire error"))
		defer srv.setFailCmd("EXPIRE", nil)

		if err := AddNotificationToZSET(ctx, 42, 9999, 12345); err == nil {
			t.Fatal("AddNotificationToZSET() error = nil, want non-nil on EXPIRE failure")
		}
	})

	t.Run("GetNotificationIDsFromZSET returns internal error when ZREVRANGE fails", func(t *testing.T) {
		srv.setFailCmd("ZREVRANGE", errors.New("zrevrange error"))
		defer srv.setFailCmd("ZREVRANGE", nil)

		_, err := GetNotificationIDsFromZSET(ctx, 42, 0, 10)
		if err == nil {
			t.Fatal("GetNotificationIDsFromZSET() error = nil, want non-nil")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("GetNotificationIDsFromZSET() error = %v, want 500 AppError", err)
		}
	})

	t.Run("GetNotificationIDsFromZSET returns error when RefreshTTL fails on non-empty result", func(t *testing.T) {
		_ = AddNotificationToZSET(ctx, 77, 5001, 1000)
		srv.setFailCmd("EXPIRE", errors.New("expire error"))
		defer srv.setFailCmd("EXPIRE", nil)

		_, err := GetNotificationIDsFromZSET(ctx, 77, 0, 10)
		if err == nil {
			t.Fatal("GetNotificationIDsFromZSET() error = nil, want non-nil when RefreshTTL fails")
		}
	})
}

func TestPurgeNotificationsZSET(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("deletes notification ZSET for user", func(t *testing.T) {
		_ = AddNotificationToZSET(ctx, 88, 101, 1000)
		if err := PurgeNotificationsZSET(ctx, 88); err != nil {
			t.Fatalf("PurgeNotificationsZSET(88) error: %v", err)
		}

		ids, err := GetNotificationIDsFromZSET(ctx, 88, 0, 10)
		if err != nil {
			t.Fatalf("GetNotificationIDsFromZSET(88) error: %v", err)
		}
		if len(ids) != 0 {
			t.Errorf("GetNotificationIDsFromZSET(88) after purge = %v, want empty", ids)
		}
	})

	t.Run("returns internal error when DEL fails", func(t *testing.T) {
		srv.setFailCmd("DEL", errors.New("del failure"))
		defer srv.setFailCmd("DEL", nil)

		err := PurgeNotificationsZSET(ctx, 88)
		if err == nil {
			t.Fatal("PurgeNotificationsZSET() error = nil, want non-nil")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("PurgeNotificationsZSET() error = %v, want 500 AppError", err)
		}
	})
}

func TestTouchActivityTimestamp(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("stores and returns current millisecond timestamp", func(t *testing.T) {
		before := time.Now().UnixMilli()
		ts := TouchActivityTimestamp(ctx, 99)
		after := time.Now().UnixMilli()

		if ts < before || ts > after {
			t.Errorf("TouchActivityTimestamp(99) = %d, want between %d and %d", ts, before, after)
		}

		stored, err := repoRedis.NotificationActivity.GetInt64(ctx, 99)
		if err != nil {
			t.Fatalf("NotificationActivity.GetInt64(99) error: %v", err)
		}
		if stored != ts {
			t.Errorf("stored NotificationActivity = %d, want %d", stored, ts)
		}
	})

	t.Run("still returns valid timestamp even if SET fails", func(t *testing.T) {
		srv.setFailCmd("SET", errors.New("set failure"))
		defer srv.setFailCmd("SET", nil)

		ts := TouchActivityTimestamp(ctx, 100)
		if ts <= 0 {
			t.Errorf("TouchActivityTimestamp() on SET error = %d, want > 0", ts)
		}
	})
}
