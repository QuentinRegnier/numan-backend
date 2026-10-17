package object_cache_service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/notification_models"
	"go.mongodb.org/mongo-driver/bson"
)

func TestSetAndGetNotificationsView(t *testing.T) {
	srv := setupTestRedis(t)
	mongoSrv := setupTestMongo(t)
	ctx := context.Background()

	// Empty slice returns empty without error
	empty, err := GetNotificationsView(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected empty slice for nil input, got %v (err=%v)", empty, err)
	}

	// Notification 1 in L1 Redis
	n1 := notification_models.NotificationPayload{
		ID:        1,
		UserID:    100,
		ActorID:   201,
		Type:      "post_like",
		TargetID:  5001,
		IsRead:    false,
		CreatedAt: 1700000010000,
	}
	if err := SetNotificationInObjectCache(ctx, n1); err != nil {
		t.Fatalf("SetNotificationInObjectCache failed: %v", err)
	}

	// Notification 2 in L2 MongoDB ("activity.notifications")
	n2Expected := notification_models.NotificationPayload{
		ID:        2,
		UserID:    100,
		ActorID:   202,
		Type:      "new_message",
		TargetID:  6002,
		IsRead:    true,
		CreatedAt: 1700000020000,
	}
	mongoSrv.setDocs("activity.notifications", bson.M{
		"id":         n2Expected.ID,
		"user_id":    n2Expected.UserID,
		"actor_id":   n2Expected.ActorID,
		"type":       n2Expected.Type,
		"target_id":  n2Expected.TargetID,
		"is_read":    n2Expected.IsRead,
		"created_at": n2Expected.CreatedAt,
	})

	// Request [2, 1, 999] -> should return [2, 1] in exact requested order
	res, err := GetNotificationsView(ctx, []int64{2, 1, 999})
	if err != nil {
		t.Fatalf("GetNotificationsView failed: %v", err)
	}
	if len(res) != 2 || res[0] != n2Expected || res[1] != n1 {
		t.Fatalf("expected notifications [%+v, %+v], got %+v", n2Expected, n1, res)
	}

	// Wait briefly for async L2 -> L1 promotion of notification 2
	time.Sleep(30 * time.Millisecond)

	// Disable Mongo and verify notification 2 is now served from L1 with all fields intact
	mongoSrv.setFailCollection("activity.notifications", true)
	resAfter, err := GetNotificationsView(ctx, []int64{2})
	if err != nil || len(resAfter) != 1 || resAfter[0] != n2Expected {
		t.Errorf("expected notification 2 promoted to L1 (%+v), got %+v (err=%v)", n2Expected, resAfter, err)
	}

	// Error branch on SetNotificationInObjectCache
	srv.setFailCmd("SET", errors.New("redis set err"))
	if err := SetNotificationInObjectCache(ctx, n1); err == nil {
		t.Errorf("expected error when SET fails, got nil")
	}
}
