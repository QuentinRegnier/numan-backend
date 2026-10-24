package object_cache_service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
)

func TestConversationObjectCache_SetAndGet(t *testing.T) {
	srv := test.SetupTestRedis(t)
	ctx := context.Background()

	// Cache miss returns 404 CodeNotFound AppError
	_, errMiss := GetConversationFromObjectCache(ctx, 404)
	assertAppError(t, errMiss, http.StatusNotFound, numan_error.CodeNotFound)

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

	// Error branches
	srv.SetFailCmd("GET", errors.New("get failed"))
	_, errGet := GetConversationFromObjectCache(ctx, 700)
	assertAppError(t, errGet, http.StatusInternalServerError, numan_error.CodeCacheError)
	srv.SetFailCmd("GET", nil)

	srv.SetFailCmd("SET", errors.New("set failed"))
	errSet := SetConversationInObjectCache(ctx, conv)
	assertAppError(t, errSet, http.StatusInternalServerError, numan_error.CodeCacheError)
}
