package object_cache_service

import (
	"context"
	"errors"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
)

func TestConversationObjectCache_SetAndGet(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	// Cache miss returns error
	if _, err := GetConversationFromObjectCache(ctx, 404); err == nil {
		t.Fatalf("expected error on cache miss, got nil")
	}

	conv := conversation_models.ConversationPayload{
		ID:            700,
		Type:          1,
		Title:         "Architecture Group",
		Description:   "Core backend discussion",
		AvatarID:      42,
		LastMessageID: 8888,
		State:         0,
		Settings:      conversation_models.DefaultConversationSettings(1),
		CreatedAt:     1700000010000,
		UpdatedAt:     1700000020000,
	}

	if err := SetConversationInObjectCache(ctx, conv); err != nil {
		t.Fatalf("SetConversationInObjectCache failed: %v", err)
	}

	got, err := GetConversationFromObjectCache(ctx, 700)
	if err != nil {
		t.Fatalf("GetConversationFromObjectCache failed: %v", err)
	}
	if got.ID != 700 || got.Title != "Architecture Group" || got.LastMessageID != 8888 || !got.Settings.SendMediaPermission || got.CreatedAt != conv.CreatedAt {
		t.Errorf("GetConversationFromObjectCache = %+v, want %+v", got, conv)
	}

	// Error branch
	srv.setFailCmd("SET", errors.New("set failed"))
	if err := SetConversationInObjectCache(ctx, conv); err == nil {
		t.Errorf("expected error when SET fails, got nil")
	}
}
