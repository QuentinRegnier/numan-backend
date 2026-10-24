package object_cache_service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/notification_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoMongo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestSetAndGetNotificationsView(t *testing.T) {
	ctx := context.Background()

	// Empty slice returns empty without error
	test.SetupTestRedis(t)
	empty, err := GetNotificationsView(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected empty slice for nil input, got %v (err=%v)", empty, err)
	}

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("L1, L2 cascade, corrupt MsgPack fallback, and MGET failure fallback", func(mt *mtest.T) {
		srv := test.SetupTestRedis(mt.T)
		restore := test.ResetCollections()
		defer restore()
		repoMongo.Notifications = test.NewMockCollection(mt, "activity.notifications", nil)

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
			mt.Fatalf("SetNotificationInObjectCache failed: %v", err)
		}

		// Notification 2 in L1 with corrupt MsgPack -> falls back to L2
		_ = repoRedis.NotificationsObj.SetPrimitive(ctx, int64(2), "\xff\xfe\xfdcorrupt")

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
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.activity.notifications", mtest.FirstBatch, bson.D{
			{Key: "id", Value: n2Expected.ID},
			{Key: "user_id", Value: n2Expected.UserID},
			{Key: "actor_id", Value: n2Expected.ActorID},
			{Key: "type", Value: n2Expected.Type},
			{Key: "target_id", Value: n2Expected.TargetID},
			{Key: "is_read", Value: n2Expected.IsRead},
			{Key: "created_at", Value: n2Expected.CreatedAt},
		}))

		// Request [2, 1, 999] -> should return [2, 1] in exact requested order
		res, err := GetNotificationsView(ctx, []int64{2, 1, 999})
		if err != nil {
			mt.Fatalf("GetNotificationsView failed: %v", err)
		}
		if len(res) != 2 || res[0] != n2Expected || res[1] != n1 {
			mt.Fatalf("expected notifications [%+v, %+v], got %+v", n2Expected, n1, res)
		}

		// Wait with bounded deadline polling for async L2 -> L1 promotion of notification 2
		waitForCondition(mt, 250*time.Millisecond, func() bool {
			var promoted notification_models.NotificationPayload
			return repoRedis.NotificationsObj.GetObject(ctx, int64(2), &promoted) == nil && promoted == n2Expected
		})

		// Verify notification 2 is now served from L1 without hitting Mongo
		resAfter, err := GetNotificationsView(ctx, []int64{2})
		if err != nil || len(resAfter) != 1 || resAfter[0] != n2Expected {
			mt.Errorf("expected notification 2 promoted to L1 (%+v), got %+v (err=%v)", n2Expected, resAfter, err)
		}

		// MGET failure falls back to L2
		srv.SetFailCmd("MGET", errors.New("mget err"))
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.activity.notifications", mtest.FirstBatch, bson.D{
			{Key: "id", Value: n2Expected.ID},
			{Key: "user_id", Value: n2Expected.UserID},
			{Key: "actor_id", Value: n2Expected.ActorID},
			{Key: "type", Value: n2Expected.Type},
			{Key: "target_id", Value: n2Expected.TargetID},
			{Key: "is_read", Value: n2Expected.IsRead},
			{Key: "created_at", Value: n2Expected.CreatedAt},
		}))
		resMGetErr, err := GetNotificationsView(ctx, []int64{2})
		srv.SetFailCmd("MGET", nil)
		if err != nil || len(resMGetErr) != 1 || resMGetErr[0] != n2Expected {
			mt.Errorf("expected L2 fallback on MGET failure, got %+v (err=%v)", resMGetErr, err)
		}

		// Error branch on SetNotificationInObjectCache
		srv.SetFailCmd("SET", errors.New("redis set err"))
		assertAppError(mt, SetNotificationInObjectCache(ctx, n1), http.StatusInternalServerError, numan_error.CodeCacheError)
		srv.SetFailCmd("SET", nil)
	})
}
