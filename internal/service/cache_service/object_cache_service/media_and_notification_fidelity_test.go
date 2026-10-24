package object_cache_service

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/notification_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/saved_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoMongo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMediaObjectCache_CompletePayloadFidelityAndErrors(t *testing.T) {
	srv := test.SetupTestRedis(t)
	ctx := context.Background()

	t.Run("SetMediaInObjectCache and GetMediaFromObjectCache preserve all MediaPayload fields", func(t *testing.T) {
		want := media_models.MediaPayload{
			ID:          101,
			OwnerID:     10,
			StoragePath: "s3://numan-media/users/10/avatar_101.webp",
			Visibility:  true,
			CreatedAt:   1700000001000,
			UpdatedAt:   1700000002000,
		}

		if err := SetMediaInObjectCache(ctx, want); err != nil {
			t.Fatalf("SetMediaInObjectCache() unexpected error: %v", err)
		}

		got, err := GetMediaFromObjectCache(ctx, want.ID)
		if err != nil {
			t.Fatalf("GetMediaFromObjectCache(%d) unexpected error: %v", want.ID, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("GetMediaFromObjectCache(%d) = %+v, want %+v", want.ID, got, want)
		}
	})

	t.Run("GetMediaFromObjectCache rejects soft-deleted media with 404 AppError", func(t *testing.T) {
		softDeleted := media_models.MediaPayload{
			ID:          102,
			OwnerID:     10,
			StoragePath: "s3://numan-media/users/10/deleted_102.webp",
			Visibility:  false,
			CreatedAt:   1700000003000,
			UpdatedAt:   1700000004000,
		}
		if err := SetMediaInObjectCache(ctx, softDeleted); err != nil {
			t.Fatalf("SetMediaInObjectCache() unexpected error: %v", err)
		}

		_, err := GetMediaFromObjectCache(ctx, softDeleted.ID)
		assertAppError(t, err, http.StatusNotFound, numan_error.CodeNotFound)
	})

	t.Run("DeleteMediaFromObjectCache evicts entry and propagates SET/DEL errors as 500 AppError", func(t *testing.T) {
		if err := DeleteMediaFromObjectCache(ctx, 101); err != nil {
			t.Fatalf("DeleteMediaFromObjectCache(101) unexpected error: %v", err)
		}
		_, errMiss := GetMediaFromObjectCache(ctx, 101)
		assertAppError(t, errMiss, http.StatusNotFound, numan_error.CodeNotFound)

		srv.SetFailCmd("SET", errors.New("redis set failure"))
		errSet := SetMediaInObjectCache(ctx, media_models.MediaPayload{
			ID:          103,
			OwnerID:     10,
			StoragePath: "s3://numan-media/users/10/fail.webp",
			Visibility:  true,
		})
		srv.SetFailCmd("SET", nil)
		assertAppError(t, errSet, http.StatusInternalServerError, numan_error.CodeCacheError)

		srv.SetFailCmd("DEL", errors.New("redis del failure"))
		errDel := DeleteMediaFromObjectCache(ctx, 101)
		srv.SetFailCmd("DEL", nil)
		assertAppError(t, errDel, http.StatusInternalServerError, numan_error.CodeCacheError)
	})
}

func TestNotificationObjectCache_CompletePayloadAndBSONFidelity(t *testing.T) {
	ctx := context.Background()

	t.Run("GetNotificationsView returns empty non-nil slice on empty input", func(t *testing.T) {
		test.SetupTestRedis(t)
		empty, err := GetNotificationsView(ctx, nil)
		if err != nil {
			t.Fatalf("GetNotificationsView(nil) error = %v, want nil", err)
		}
		if empty == nil || len(empty) != 0 {
			t.Errorf("GetNotificationsView(nil) = %v, want empty non-nil slice", empty)
		}
	})

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("hydrates L1 MsgPack and L2 MongoDB BSON with full NotificationPayload fidelity and promotes L2 to L1", func(mt *mtest.T) {
		srv := test.SetupTestRedis(mt.T)
		restore := test.ResetCollections()
		defer restore()
		repoMongo.Notifications = test.NewMockCollection(mt, "activity.notifications", nil)

		notifL1 := notification_models.NotificationPayload{
			ID:        1,
			UserID:    100,
			ActorID:   201,
			Type:      "post_like",
			TargetID:  5001,
			IsRead:    false,
			CreatedAt: 1700000010000,
		}
		if err := SetNotificationInObjectCache(ctx, notifL1); err != nil {
			mt.Fatalf("SetNotificationInObjectCache(1) unexpected error: %v", err)
		}

		notifL2Expected := notification_models.NotificationPayload{
			ID:        2,
			UserID:    100,
			ActorID:   202,
			Type:      "new_message",
			TargetID:  6002,
			IsRead:    true,
			CreatedAt: 1700000020000,
		}
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.activity.notifications", mtest.FirstBatch, bson.D{
			{Key: "id", Value: notifL2Expected.ID},
			{Key: "user_id", Value: notifL2Expected.UserID},
			{Key: "actor_id", Value: notifL2Expected.ActorID},
			{Key: "type", Value: notifL2Expected.Type},
			{Key: "target_id", Value: notifL2Expected.TargetID},
			{Key: "is_read", Value: notifL2Expected.IsRead},
			{Key: "created_at", Value: notifL2Expected.CreatedAt},
		}))

		// Request [2, 1, 999] -> should return [2, 1] preserving caller order and all struct fields
		got, err := GetNotificationsView(ctx, []int64{2, 1, 999})
		if err != nil {
			mt.Fatalf("GetNotificationsView([2, 1, 999]) unexpected error: %v", err)
		}
		if len(got) != 2 {
			mt.Fatalf("GetNotificationsView() len = %d, want 2 (got %+v)", len(got), got)
		}
		if !reflect.DeepEqual(got[0], notifL2Expected) {
			mt.Errorf("got[0] (from L2 BSON) = %+v, want %+v", got[0], notifL2Expected)
		}
		if !reflect.DeepEqual(got[1], notifL1) {
			mt.Errorf("got[1] (from L1 MsgPack) = %+v, want %+v", got[1], notifL1)
		}

		// Wait for async L2 -> L1 promotion goroutine and verify L1 now serves notification 2
		waitForCondition(mt, 250*time.Millisecond, func() bool {
			ok, _ := repoRedis.NotificationsObj.Exists(ctx, int64(2))
			return ok
		})

		resPromoted, err := GetNotificationsView(ctx, []int64{2})
		if err != nil || len(resPromoted) != 1 {
			mt.Fatalf("GetNotificationsView([2]) after L1 promotion = (%+v, %v), want 1 item", resPromoted, err)
		}
		if !reflect.DeepEqual(resPromoted[0], notifL2Expected) {
			mt.Errorf("promoted notification 2 = %+v, want %+v", resPromoted[0], notifL2Expected)
		}

		// SetNotificationInObjectCache returns 500 AppError when Redis SET fails
		srv.SetFailCmd("SET", errors.New("redis set error"))
		errSet := SetNotificationInObjectCache(ctx, notification_models.NotificationPayload{
			ID:        3,
			UserID:    100,
			ActorID:   203,
			Type:      "new_follower",
			TargetID:  100,
			IsRead:    false,
			CreatedAt: 1700000030000,
		})
		srv.SetFailCmd("SET", nil)
		assertAppError(mt, errSet, http.StatusInternalServerError, numan_error.CodeCacheError)
	})
}

func TestObjectCacheAllPayloads_RoundTripFidelity(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()

	t.Run("PostPayload complete 22-field round-trip in L1 Object Cache", func(t *testing.T) {
		wantPost := post_models.PostPayload{
			ID:                8801,
			UserID:            42,
			Content:           "Complete 22-field post payload",
			Hashtags:          []string{"golang", "testing"},
			IndirectHashtags:  []string{"backend"},
			Identifiers:       []int64{100, 200},
			MediaIDs:          []int64{501},
			Visibility:        1,
			PriorityLevel:     2,
			Location:          "Paris, France",
			LikeCount:         15,
			CommentCount:      4,
			ViewCount:         120,
			ReportCount:       0,
			HasMedia:          true,
			Vector:            []float32{0.25, 0.5, 0.75},
			VectorVersion:     1,
			TelemetryDwellSum: 45.5,
			TelemetryDwellSq:  310.25,
			TelemetryClicks:   9,
			CreatedAt:         1700000100000,
			UpdatedAt:         1700000105000,
		}

		if err := SetPostInObjectCache(ctx, wantPost); err != nil {
			t.Fatalf("SetPostInObjectCache() error: %v", err)
		}
		gotPost, err := GetPostFromObjectCache(ctx, wantPost.ID)
		if err != nil {
			t.Fatalf("GetPostFromObjectCache() error: %v", err)
		}
		if !reflect.DeepEqual(gotPost, wantPost) {
			t.Errorf("GetPostFromObjectCache() = %+v, want %+v", gotPost, wantPost)
		}
	})

	t.Run("SavedPayload ZSET lifecycle via AddSavedToZSET, GetSavedPostIDs, and RemoveSavedFromZSET", func(t *testing.T) {
		savedEntry := saved_models.SavedPayload{
			ID:        9901,
			UserID:    42,
			PostID:    8801,
			CreatedAt: 1700000200000,
		}
		if err := AddSavedToZSET(ctx, savedEntry.UserID, savedEntry.PostID, float64(savedEntry.CreatedAt)); err != nil {
			t.Fatalf("AddSavedToZSET() error: %v", err)
		}
		gotIDs, err := GetSavedPostIDs(ctx, savedEntry.UserID, 0, 10)
		if err != nil {
			t.Fatalf("GetSavedPostIDs() error: %v", err)
		}
		if !reflect.DeepEqual(gotIDs, []int64{savedEntry.PostID}) {
			t.Errorf("GetSavedPostIDs() = %v, want [%d]", gotIDs, savedEntry.PostID)
		}
		if err := RemoveSavedFromZSET(ctx, savedEntry.UserID, savedEntry.PostID); err != nil {
			t.Fatalf("RemoveSavedFromZSET() error: %v", err)
		}
		gotAfterRem, err := GetSavedPostIDs(ctx, savedEntry.UserID, 0, 10)
		if err != nil || len(gotAfterRem) != 0 {
			t.Errorf("GetSavedPostIDs() after RemoveSavedFromZSET = (%v, %v), want empty", gotAfterRem, err)
		}
	})
}
