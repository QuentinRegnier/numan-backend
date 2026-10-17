package object_cache_service

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestSavedPostsZSET_AddRemoveAndGet(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	userID := int64(77)

	// Empty ZSET returns empty slice
	empty, err := GetSavedPostIDs(ctx, userID, 0, 10)
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected empty slice for user with no saved posts, got %v (err=%v)", empty, err)
	}

	if err := AddSavedToZSET(ctx, userID, 1001, 100.0); err != nil {
		t.Fatalf("AddSavedToZSET(1001) failed: %v", err)
	}
	if err := AddSavedToZSET(ctx, userID, 1002, 300.0); err != nil {
		t.Fatalf("AddSavedToZSET(1002) failed: %v", err)
	}
	if err := AddSavedToZSET(ctx, userID, 1003, 200.0); err != nil {
		t.Fatalf("AddSavedToZSET(1003) failed: %v", err)
	}

	ids, err := GetSavedPostIDs(ctx, userID, 0, 10)
	if err != nil {
		t.Fatalf("GetSavedPostIDs failed: %v", err)
	}
	if !reflect.DeepEqual(ids, []int64{1002, 1003, 1001}) {
		t.Errorf("GetSavedPostIDs = %v, want [1002 1003 1001]", ids)
	}

	// Pagination check (offset 1, limit 1 -> [1003])
	paged, err := GetSavedPostIDs(ctx, userID, 1, 1)
	if err != nil || !reflect.DeepEqual(paged, []int64{1003}) {
		t.Errorf("GetSavedPostIDs(offset=1, limit=1) = %v (err=%v), want [1003]", paged, err)
	}

	// RemoveSavedFromZSET
	if err := RemoveSavedFromZSET(ctx, userID, 1003); err != nil {
		t.Fatalf("RemoveSavedFromZSET(1003) failed: %v", err)
	}
	idsAfter, _ := GetSavedPostIDs(ctx, userID, 0, 10)
	if !reflect.DeepEqual(idsAfter, []int64{1002, 1001}) {
		t.Errorf("GetSavedPostIDs after remove = %v, want [1002 1001]", idsAfter)
	}

	// Error branches
	srv.setFailCmd("ZADD", errors.New("zadd err"))
	if err := AddSavedToZSET(ctx, userID, 1004, 400.0); err == nil {
		t.Errorf("expected error when ZADD fails")
	}
	srv.setFailCmd("ZADD", nil)

	srv.setFailCmd("ZREM", errors.New("zrem err"))
	if err := RemoveSavedFromZSET(ctx, userID, 1001); err == nil {
		t.Errorf("expected error when ZREM fails")
	}
	srv.setFailCmd("ZREM", nil)

	srv.setFailCmd("ZREVRANGE", errors.New("zrevrange err"))
	if _, err := GetSavedPostIDs(ctx, userID, 0, 10); err == nil {
		t.Errorf("expected error when ZREVRANGE fails")
	}
}
