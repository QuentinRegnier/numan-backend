package object_cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"go.mongodb.org/mongo-driver/bson"
)

func TestMessageObjectCache_CRUD(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

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
	if _, err := GetMessageFromObjectCache(ctx, 1001); err == nil {
		t.Errorf("expected cache miss after DeleteMessageFromObjectCache, got nil")
	}

	// Error branches
	srv.setFailCmd("SET", errors.New("set err"))
	if err := SetMessageInObjectCache(ctx, msg); err == nil {
		t.Errorf("expected error when SET fails, got nil")
	}
	srv.setFailCmd("SET", nil)

	srv.setFailCmd("DEL", errors.New("del err"))
	if err := DeleteMessageFromObjectCache(ctx, 1001); err == nil {
		t.Errorf("expected error when DEL fails, got nil")
	}
}

func TestGetMessagesView_HybridCascadeAndVisibilityFilter(t *testing.T) {
	setupTestRedis(t)
	mongoSrv := setupTestMongo(t)
	ctx := context.Background()

	// Empty input returns empty slice
	empty, err := GetMessagesView(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected empty slice for nil input, got %v (err=%v)", empty, err)
	}

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

	// Message 2 in L2 MongoDB ("messaging.messages")
	mongoSrv.setDocs("messaging.messages", bson.M{
		"id":                  int64(2),
		"conversation_id":     int64(100),
		"sender_id":           int64(11),
		"message_type":        int32(0),
		"content":             "Msg 2 L2",
		"visibility":          true,
		"allow_new_reactions": true,
		"created_at":          now,
		"updated_at":          now,
	})

	// Message 3 in L3 PostgreSQL
	setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
		return fakeSQLResponse{
			cols: []string{"id", "conversation_id", "sender_id", "message_type", "visibility", "content", "attachments", "allow_new_reactions", "created_at", "updated_at"},
			rows: [][]driver.Value{{
				int64(3), int64(100), int64(10), int64(0), true, "Msg 3 L3", []byte(`{"type":"text"}`), true, now, now,
			}},
		}
	})

	// Request [3, 4, 2, 1] -> 4 is filtered out because Visibility==false; order should be [3, 2, 1]
	res, err := GetMessagesView(ctx, []int64{3, 4, 2, 1})
	if err != nil {
		t.Fatalf("GetMessagesView failed: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("expected 3 visible messages, got %d (%+v)", len(res), res)
	}
	expectedOrder := []int64{3, 2, 1}
	for i, expID := range expectedOrder {
		if res[i].ID != expID {
			t.Errorf("at index %d expected message ID %d, got %d", i, expID, res[i].ID)
		}
	}

	// Wait briefly for async L2/L3 -> L1 promotions
	time.Sleep(30 * time.Millisecond)

	if _, err := GetMessageFromObjectCache(ctx, 2); err != nil {
		t.Errorf("expected message 2 promoted to L1, got err: %v", err)
	}
	if _, err := GetMessageFromObjectCache(ctx, 3); err != nil {
		t.Errorf("expected message 3 promoted to L1, got err: %v", err)
	}
}
