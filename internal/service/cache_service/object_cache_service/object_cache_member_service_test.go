package object_cache_service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
)

func TestMemberObjectCache_SetAndGet(t *testing.T) {
	srv := test.SetupTestRedis(t)
	ctx := context.Background()

	// Cache miss returns 404 CodeNotFound AppError
	_, errMiss := GetMemberFromObjectCache(ctx, 10, 20)
	assertAppError(t, errMiss, http.StatusNotFound, numan_error.CodeNotFound)

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

	// Error branches
	srv.SetFailCmd("GET", errors.New("get failed"))
	_, errGet := GetMemberFromObjectCache(ctx, 10, 20)
	assertAppError(t, errGet, http.StatusInternalServerError, numan_error.CodeCacheError)
	srv.SetFailCmd("GET", nil)

	srv.SetFailCmd("SET", errors.New("set failed"))
	errSet := SetMemberInObjectCache(ctx, mem)
	assertAppError(t, errSet, http.StatusInternalServerError, numan_error.CodeCacheError)
}
