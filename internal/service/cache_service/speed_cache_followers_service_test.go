package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"go.mongodb.org/mongo-driver/bson"
)

func TestRelationValueCascade(t *testing.T) {
	ctx := context.Background()

	t.Run("L1 cache hit returns state directly without querying L2 or L3", func(t *testing.T) {
		setupTestRedis(t)
		_ = repoRedis.SpeedRelations.HSet(ctx, int64(200), "100", variables.RelationStateFriend)

		got := RelationValue(ctx, 200, 100)
		if got != variables.RelationStateFriend {
			t.Fatalf("RelationValue(target=200, caller=100) = %d, want %d", got, variables.RelationStateFriend)
		}
	})

	t.Run("L1 miss -> L2 MongoDB hit rehydrates L1 and returns state", func(t *testing.T) {
		setupTestRedis(t)
		mSrv := setupTestMongo(t)
		mSrv.setDocs("auth.relations", bson.M{
			"caller_id": int64(100),
			"target_id": int64(201),
			"state":     int32(variables.RelationStateFollow),
		})

		got := RelationValue(ctx, 201, 100)
		if got != variables.RelationStateFollow {
			t.Fatalf("RelationValue() from L2 = %d, want %d", got, variables.RelationStateFollow)
		}

		// Verify L1 was rehydrated
		cached, err := repoRedis.SpeedRelations.HGet(ctx, int64(201), "100").Result()
		if err != nil || cached != "1" {
			t.Errorf("expected L1 SpeedRelations HSET to be '1', got (%q, %v)", cached, err)
		}
	})

	t.Run("L1 and L2 miss -> L3 Postgres hit rehydrates L1 and enqueues L2", func(t *testing.T) {
		setupTestRedis(t)
		mSrv := setupTestMongo(t)
		mSrv.setDocs("auth.relations") // empty L2

		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			return fakeSQLResponse{
				cols: []string{"func_get_relation_state"},
				rows: [][]driver.Value{{int64(variables.RelationStateBlocked)}},
			}
		})

		got := RelationValue(ctx, 202, 100)
		if got != variables.RelationStateBlocked {
			t.Fatalf("RelationValue() from L3 = %d, want %d", got, variables.RelationStateBlocked)
		}

		cached, err := repoRedis.SpeedRelations.HGet(ctx, int64(202), "100").Result()
		if err != nil || cached != "-1" {
			t.Errorf("expected L1 SpeedRelations HSET to be '-1', got (%q, %v)", cached, err)
		}
		// Allow async L2 enqueue goroutine to complete
		time.Sleep(15 * time.Millisecond)
	})

	t.Run("L1, L2, and L3 failure returns safe default RelationStateNone", func(t *testing.T) {
		setupTestRedis(t)
		mSrv := setupTestMongo(t)
		mSrv.setFailCollection("auth.relations", true)

		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			return fakeSQLResponse{err: errors.New("postgres down")}
		})

		got := RelationValue(ctx, 203, 100)
		if got != variables.RelationStateNone {
			t.Fatalf("RelationValue() on complete failure = %d, want %d", got, variables.RelationStateNone)
		}
	})
}

func TestUpdateRelationStateAndIndexes(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("transitions between Follow, Friend, Blocked, and None cleanly without duplicates", func(t *testing.T) {
		targetID := int64(500)
		callerID := int64(10)

		// 1. Follow
		if err := UpdateRelationState(ctx, targetID, callerID, variables.RelationStateFollow, 1000); err != nil {
			t.Fatalf("UpdateRelationState(Follow) error: %v", err)
		}
		if count := GetFollowerCount(ctx, targetID); count != 1 {
			t.Errorf("GetFollowerCount() = %d, want 1", count)
		}
		followers, err := GetSpeedRelationsIndex(ctx, targetID)
		if err != nil || !reflect.DeepEqual(followers, []int64{callerID}) {
			t.Errorf("GetSpeedRelationsIndex() = (%v, %v), want ([%d], nil)", followers, err, callerID)
		}

		// 2. Promote to Friend (should remove from Follow ZSET and add to Friend ZSET)
		if err := UpdateRelationState(ctx, targetID, callerID, variables.RelationStateFriend, 2000); err != nil {
			t.Fatalf("UpdateRelationState(Friend) error: %v", err)
		}
		if count := GetFollowerCount(ctx, targetID); count != 0 {
			t.Errorf("GetFollowerCount() after Friend promotion = %d, want 0", count)
		}
		friends, err := GetSpeedFriends(ctx, targetID)
		if err != nil || !reflect.DeepEqual(friends, []int64{callerID}) {
			t.Errorf("GetSpeedFriends() = (%v, %v), want ([%d], nil)", friends, err, callerID)
		}

		// 3. Block (should remove from Friend ZSET and add to caller's outgoing Blocked ZSET)
		if err := UpdateRelationState(ctx, targetID, callerID, variables.RelationStateBlocked, 3000); err != nil {
			t.Fatalf("UpdateRelationState(Blocked) error: %v", err)
		}
		friendsAfterBlock, _ := GetSpeedFriends(ctx, targetID)
		if len(friendsAfterBlock) != 0 {
			t.Errorf("GetSpeedFriends() after Block = %v, want empty", friendsAfterBlock)
		}
		blockedOut, err := GetRelationsByDirectionPaginatedFromCache(ctx, callerID, variables.RelationStateBlocked, "outgoing", 10, 0)
		if err != nil || !reflect.DeepEqual(blockedOut, []int64{targetID}) {
			t.Errorf("GetRelationsByDirectionPaginatedFromCache(outgoing) = (%v, %v), want ([%d], nil)", blockedOut, err, targetID)
		}

		// 4. Reset to None (removes from all ZSETs)
		if err := UpdateRelationState(ctx, targetID, callerID, variables.RelationStateNone, 4000); err != nil {
			t.Fatalf("UpdateRelationState(None) error: %v", err)
		}
		blockedAfterNone, _ := GetRelationsByDirectionPaginatedFromCache(ctx, callerID, variables.RelationStateBlocked, "outgoing", 10, 0)
		if len(blockedAfterNone) != 0 {
			t.Errorf("blocked list after None = %v, want empty", blockedAfterNone)
		}
	})

	t.Run("GetRelationsByDirectionPaginatedFromCache supports incoming direction and pagination", func(t *testing.T) {
		targetID := int64(600)
		_ = UpdateRelationState(ctx, targetID, 1, variables.RelationStateFollow, 100)
		_ = UpdateRelationState(ctx, targetID, 2, variables.RelationStateFollow, 200)
		_ = UpdateRelationState(ctx, targetID, 3, variables.RelationStateFollow, 300)

		page, err := GetRelationsByDirectionPaginatedFromCache(ctx, targetID, variables.RelationStateFollow, "incoming", 2, 1)
		if err != nil {
			t.Fatalf("GetRelationsByDirectionPaginatedFromCache(incoming) error: %v", err)
		}
		want := []int64{2, 1}
		if !reflect.DeepEqual(page, want) {
			t.Errorf("GetRelationsByDirectionPaginatedFromCache(incoming, limit=2, offset=1) = %v, want %v", page, want)
		}
	})

	t.Run("error propagation when Redis commands fail", func(t *testing.T) {
		srv.setFailCmd("HSET", errors.New("hset failure"))
		err := UpdateRelationState(ctx, 1, 2, variables.RelationStateFollow, 100)
		srv.setFailCmd("HSET", nil)
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("UpdateRelationState() on HSET error = %v, want 500 AppError", err)
		}

		srv.setFailCmd("ZREVRANGE", fmt.Errorf("zrevrange error"))
		if _, err := GetSpeedRelationsIndex(ctx, 1); err == nil {
			t.Error("GetSpeedRelationsIndex() expected error on ZREVRANGE failure")
		}
		if _, err := GetSpeedFriends(ctx, 1); err == nil {
			t.Error("GetSpeedFriends() expected error on ZREVRANGE failure")
		}
		if _, err := GetRelationsByDirectionPaginatedFromCache(ctx, 1, 1, "incoming", 10, 0); err == nil {
			t.Error("GetRelationsByDirectionPaginatedFromCache() expected error on ZREVRANGE failure")
		}
		srv.setFailCmd("ZREVRANGE", nil)
	})
}
