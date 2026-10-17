package cache_service

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

func TestSyncLedgerOperations(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("RecordConversationMutation records conversation across all participants and GetModifiedConversationIDs filters strictly > sinceTimestamp", func(t *testing.T) {
		beforeMs := int64(1000)
		participants := []int64{10, 20, 30}

		if err := RecordConversationMutation(ctx, 500, participants); err != nil {
			t.Fatalf("RecordConversationMutation() unexpected error: %v", err)
		}

		for _, uid := range participants {
			convs, err := GetModifiedConversationIDs(ctx, uid, beforeMs)
			if err != nil {
				t.Fatalf("GetModifiedConversationIDs(user=%d) error: %v", uid, err)
			}
			if !reflect.DeepEqual(convs, []int64{500}) {
				t.Errorf("GetModifiedConversationIDs(user=%d) = %v, want [500]", uid, convs)
			}

			// Using a future timestamp should exclude the mutation (strict > threshold)
			futureConvs, err := GetModifiedConversationIDs(ctx, uid, 9999999999999)
			if err != nil {
				t.Fatalf("GetModifiedConversationIDs(future) error: %v", err)
			}
			if len(futureConvs) != 0 {
				t.Errorf("GetModifiedConversationIDs(future) = %v, want empty", futureConvs)
			}
		}
	})

	t.Run("RecordMessageMutation updates message ledger and triggers conversation mutation", func(t *testing.T) {
		convID := int64(600)
		msgID := int64(9001)
		participants := []int64{11, 22}

		if err := RecordMessageMutation(ctx, convID, msgID, participants); err != nil {
			t.Fatalf("RecordMessageMutation() unexpected error: %v", err)
		}

		msgs, err := GetModifiedMessageIDs(ctx, convID, 0)
		if err != nil {
			t.Fatalf("GetModifiedMessageIDs() error: %v", err)
		}
		if !reflect.DeepEqual(msgs, []int64{msgID}) {
			t.Errorf("GetModifiedMessageIDs() = %v, want [%d]", msgs, msgID)
		}

		for _, uid := range participants {
			convs, err := GetModifiedConversationIDs(ctx, uid, 0)
			if err != nil {
				t.Fatalf("GetModifiedConversationIDs(user=%d) error: %v", uid, err)
			}
			if !reflect.DeepEqual(convs, []int64{convID}) {
				t.Errorf("GetModifiedConversationIDs(user=%d) = %v, want [%d]", uid, convs, convID)
			}
		}
	})

	t.Run("RecordConversationMutation returns internal error when ZADD pipeline fails", func(t *testing.T) {
		srv.setFailCmd("ZADD", errors.New("zadd pipeline error"))
		defer srv.setFailCmd("ZADD", nil)

		err := RecordConversationMutation(ctx, 700, []int64{1, 2})
		if err == nil {
			t.Fatal("RecordConversationMutation() error = nil, want non-nil")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("RecordConversationMutation() error = %v, want 500 AppError", err)
		}
	})

	t.Run("GetModifiedConversationIDs and GetModifiedMessageIDs return internal error on ZRANGEBYSCORE failure", func(t *testing.T) {
		srv.setFailCmd("ZRANGEBYSCORE", errors.New("zrangebyscore failure"))
		defer srv.setFailCmd("ZRANGEBYSCORE", nil)

		if _, err := GetModifiedConversationIDs(ctx, 10, 0); err == nil {
			t.Error("GetModifiedConversationIDs() error = nil, want non-nil")
		}
		if _, err := GetModifiedMessageIDs(ctx, 600, 0); err == nil {
			t.Error("GetModifiedMessageIDs() error = nil, want non-nil")
		}
	})
}
