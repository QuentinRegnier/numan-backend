package object_cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoMongo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMessageObjectCache_CRUD(t *testing.T) {
	srv := test.SetupTestRedis(t)
	ctx := context.Background()

	// Cache miss returns 404 CodeNotFound AppError
	_, errMiss := GetMessageFromObjectCache(ctx, 404)
	assertAppError(t, errMiss, http.StatusNotFound, numan_error.CodeNotFound)

	msg := message_models.MessagePayload{
		ID:             1001,
		ConversationID: 50,
		SenderID:       10,
		Content:        "Hello",
		Visibility:     true,
	}

	if err := SetMessageInObjectCache(ctx, msg); err != nil {
		t.Fatalf("SetMessageInObjectCache failed: %v", err)
	}

	got, err := GetMessageFromObjectCache(ctx, 1001)
	if err != nil {
		t.Fatalf("GetMessageFromObjectCache failed: %v", err)
	}
	if got.ID != 1001 || got.Content != "Hello" {
		t.Errorf("GetMessageFromObjectCache = %+v, want %+v", got, msg)
	}

	if err := DeleteMessageFromObjectCache(ctx, 1001); err != nil {
		t.Fatalf("DeleteMessageFromObjectCache failed: %v", err)
	}
	_, errAfterDel := GetMessageFromObjectCache(ctx, 1001)
	assertAppError(t, errAfterDel, http.StatusNotFound, numan_error.CodeNotFound)

	// Error branches
	srv.SetFailCmd("GET", errors.New("get err"))
	_, errGet := GetMessageFromObjectCache(ctx, 1001)
	assertAppError(t, errGet, http.StatusInternalServerError, numan_error.CodeCacheError)
	srv.SetFailCmd("GET", nil)

	srv.SetFailCmd("SET", errors.New("set err"))
	assertAppError(t, SetMessageInObjectCache(ctx, msg), http.StatusInternalServerError, numan_error.CodeCacheError)
	srv.SetFailCmd("SET", nil)

	srv.SetFailCmd("DEL", errors.New("del err"))
	assertAppError(t, DeleteMessageFromObjectCache(ctx, 1001), http.StatusInternalServerError, numan_error.CodeCacheError)
}

func TestGetMessagesView_HybridCascadeAndVisibilityFilter(t *testing.T) {
	ctx := context.Background()

	// Empty input returns empty slice
	test.SetupTestRedis(t)
	empty, err := GetMessagesView(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected empty slice for nil input, got %v (err=%v)", empty, err)
	}

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("L1, L2, L3 cascade, visibility filter, and corrupt MsgPack/MGET fallback", func(mt *mtest.T) {
		srv := test.SetupTestRedis(mt.T)
		restore := test.ResetCollections()
		defer restore()
		repoMongo.Messages = test.NewMockCollection(mt, "messaging.messages", nil)

		now := time.Now().UnixMilli()
		// Message 1 in L1 (visible)
		_ = SetMessageInObjectCache(ctx, message_models.MessagePayload{
			ID:                1,
			ConversationID:    100,
			SenderID:          10,
			MessageType:       0,
			Content:           "Msg 1 L1",
			Visibility:        true,
			AllowNewReactions: true,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		// Message 4 in L1 (soft-deleted, Visibility == false -> filtered out in Step 4)
		_ = SetMessageInObjectCache(ctx, message_models.MessagePayload{
			ID:                4,
			ConversationID:    100,
			SenderID:          10,
			MessageType:       0,
			Content:           "Msg 4 Deleted",
			Visibility:        false,
			AllowNewReactions: false,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		// Message 2 in L1 with corrupt MsgPack bytes -> should fall back to L2
		_ = repoRedis.Messages.SetPrimitive(ctx, int64(2), "\xff\xfe\xfdcorrupt")

		// Message 2 in L2 MongoDB ("messaging.messages")
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.messaging.messages", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(2)},
			{Key: "conversation_id", Value: int64(100)},
			{Key: "sender_id", Value: int64(11)},
			{Key: "message_type", Value: int32(0)},
			{Key: "content", Value: "Msg 2 L2"},
			{Key: "visibility", Value: true},
			{Key: "allow_new_reactions", Value: true},
			{Key: "created_at", Value: now},
			{Key: "updated_at", Value: now},
		}))

		// Message 3 in L3 PostgreSQL
		cols := []string{"id", "conversation_id", "sender_id", "message_type", "visibility", "content", "attachments", "allow_new_reactions", "created_at", "updated_at"}
		test.SetupMockDB(mt.T, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{{
				int64(3), int64(100), int64(10), int64(0), true, "Msg 3 L3", []byte(`{"type":"text"}`), true, now, now,
			}}), nil
		}, nil)

		// Request [3, 4, 2, 1] -> 4 is filtered out because Visibility==false; order should be [3, 2, 1]
		res, err := GetMessagesView(ctx, []int64{3, 4, 2, 1})
		if err != nil {
			mt.Fatalf("GetMessagesView failed: %v", err)
		}
		if len(res) != 3 {
			mt.Fatalf("expected 3 visible messages, got %d (%+v)", len(res), res)
		}
		expectedOrder := []int64{3, 2, 1}
		for i, expID := range expectedOrder {
			if res[i].ID != expID {
				mt.Errorf("at index %d expected message ID %d, got %d", i, expID, res[i].ID)
			}
		}

		// Wait with bounded deadline polling for async L2/L3 -> L1 promotions
		waitForCondition(mt, 250*time.Millisecond, func() bool {
			_, err2 := GetMessageFromObjectCache(ctx, 2)
			_, err3 := GetMessageFromObjectCache(ctx, 3)
			return err2 == nil && err3 == nil
		})

		// MGET error fallback to L2
		srv.SetFailCmd("MGET", errors.New("mget failed"))
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.messaging.messages", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(2)},
			{Key: "conversation_id", Value: int64(100)},
			{Key: "sender_id", Value: int64(11)},
			{Key: "message_type", Value: int32(0)},
			{Key: "content", Value: "Msg 2 L2"},
			{Key: "visibility", Value: true},
			{Key: "allow_new_reactions", Value: true},
			{Key: "created_at", Value: now},
			{Key: "updated_at", Value: now},
		}))
		resFallback, err := GetMessagesView(ctx, []int64{2})
		srv.SetFailCmd("MGET", nil)
		if err != nil || len(resFallback) != 1 || resFallback[0].ID != 2 {
			mt.Errorf("expected L2 fallback on MGET failure to return message 2, got %+v (err=%v)", resFallback, err)
		}
	})
}
