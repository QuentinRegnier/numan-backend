package object_cache_service

import (
	"context"
	"errors"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
)

func TestMemberObjectCache_SetAndGet(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	// Cache miss returns error
	if _, err := GetMemberFromObjectCache(ctx, 10, 20); err == nil {
		t.Fatalf("expected error on cache miss, got nil")
	}

	mem := member_models.MemberPayload{
		ID:             1,
		ConversationID: 10,
		UserID:         20,
		Role:           2,
		Settings: member_models.MemberSettings{
			IsMuted:           1,
			MuteExpireAt:      1700000900000,
			Pinned:            0,
			MediaAutoDownload: true,
			RestrictedUntil:   0,
		},
		JoinedAt:          1700000010000,
		UnreadCount:       5,
		FrozenMessageID:   0,
		LastReadMessageID: 100,
		CreatedAt:         1700000010000,
		UpdatedAt:         1700000020000,
	}

	if err := SetMemberInObjectCache(ctx, mem); err != nil {
		t.Fatalf("SetMemberInObjectCache failed: %v", err)
	}

	got, err := GetMemberFromObjectCache(ctx, 10, 20)
	if err != nil {
		t.Fatalf("GetMemberFromObjectCache failed: %v", err)
	}
	if got != mem {
		t.Errorf("GetMemberFromObjectCache = %+v, want %+v", got, mem)
	}

	// Error branch
	srv.setFailCmd("SET", errors.New("set failed"))
	if err := SetMemberInObjectCache(ctx, mem); err == nil {
		t.Errorf("expected error when SET fails, got nil")
	}
}
