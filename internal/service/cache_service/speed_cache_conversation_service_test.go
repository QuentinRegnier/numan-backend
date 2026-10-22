package cache_service

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"go.mongodb.org/mongo-driver/bson"
)

func TestGetInboxView_EmptyInbox(t *testing.T) {
	setupTestRedis(t)
	ctx := context.Background()

	items, err := GetInboxView(ctx, 10, 20, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
}

func TestGetInboxView_RedisInboxError(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	srv.setFailCmd("ZREVRANGE", errors.New("redis zrevrange error"))
	_, err := GetInboxView(ctx, 10, 20, 0)
	if err == nil {
		t.Fatalf("expected error when UserInbox ZRevRange fails, got nil")
	}
}

func TestGetInboxView_L1HitAndDynamicSorting(t *testing.T) {
	setupTestRedis(t)
	ctx := context.Background()

	userID := int64(42)

	// Conversations 100 (unpinned, msg 50), 200 (pinned index 1, msg 10), 300 (pinned index 0, msg 5), 400 (unpinned, msg 90)
	convs := []lite_models.ConvLiteRequest{
		{ID: 100, Type: 0, Title: "Conv 100", LastMessageID: 50},
		{ID: 200, Type: 1, Title: "Conv 200", LastMessageID: 10},
		{ID: 300, Type: 1, Title: "Conv 300", LastMessageID: 5},
		{ID: 400, Type: 0, Title: "Conv 400", LastMessageID: 90},
	}
	members := []lite_models.MemberLiteRequest{
		{ConversationID: 100, UserID: userID, Role: 0, Settings: lite_models.MemberSettingsLite{Pinned: -1}, UnreadCount: 1},
		{ConversationID: 200, UserID: userID, Role: 1, Settings: lite_models.MemberSettingsLite{Pinned: 1}, UnreadCount: 0},
		{ConversationID: 300, UserID: userID, Role: 2, Settings: lite_models.MemberSettingsLite{Pinned: 0}, UnreadCount: 3},
		{ConversationID: 400, UserID: userID, Role: 0, Settings: lite_models.MemberSettingsLite{Pinned: -1}, UnreadCount: 2},
	}

	for i, c := range convs {
		if err := redis.UserInbox.ZAddWithCap(ctx, userID, float64(c.LastMessageID), c.ID, 100); err != nil {
			t.Fatalf("ZAddWithCap failed: %v", err)
		}
		if err := redis.ConvMeta.SetObject(ctx, c.ID, c); err != nil {
			t.Fatalf("ConvMeta.SetObject failed: %v", err)
		}
		key := fmt.Sprintf("%d:%d", c.ID, userID)
		if err := redis.ConvMembers.SetObject(ctx, key, members[i]); err != nil {
			t.Fatalf("ConvMembers.SetObject failed: %v", err)
		}
	}

	items, err := GetInboxView(ctx, userID, 10, 0)
	if err != nil {
		t.Fatalf("GetInboxView failed: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(items))
	}

	expectedOrder := []int64{300, 200, 400, 100}
	for i, expID := range expectedOrder {
		if items[i].Conversation.ID != expID {
			t.Errorf("at index %d expected conv %d, got %d", i, expID, items[i].Conversation.ID)
		}
	}
}

func TestGetInboxView_PostgresFallbackAndSelfHealing(t *testing.T) {
	setupTestRedis(t)
	ctx := context.Background()

	userID := int64(55)
	convID := int64(777)

	if err := redis.UserInbox.ZAddWithCap(ctx, userID, 1000, convID, 100); err != nil {
		t.Fatalf("ZAddWithCap failed: %v", err)
	}

	fallbackPayload := []map[string]any{
		{
			"conversation": lite_models.ConvLiteRequest{
				ID:            convID,
				Type:          1,
				Title:         "Fallback Group",
				LastMessageID: 1000,
			},
			"member": lite_models.MemberLiteRequest{
				ConversationID: convID,
				UserID:         userID,
				Role:           1,
				Settings:       lite_models.MemberSettingsLite{Pinned: -1},
				UnreadCount:    4,
			},
		},
	}
	jsonBytes, err := json.Marshal(fallbackPayload)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
		return fakeSQLResponse{
			cols: []string{"json"},
			rows: [][]driver.Value{{jsonBytes}},
		}
	})

	items, err := GetInboxView(ctx, userID, 10, 0)
	if err != nil {
		t.Fatalf("GetInboxView failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item from fallback, got %d", len(items))
	}
	if items[0].Conversation.Title != "Fallback Group" || items[0].Member.UnreadCount != 4 {
		t.Errorf("unexpected fallback item: %+v", items[0])
	}

	time.Sleep(30 * time.Millisecond)

	var healedMeta lite_models.ConvLiteRequest
	if err := redis.ConvMeta.GetObject(ctx, convID, &healedMeta); err != nil {
		t.Fatalf("expected ConvMeta to be healed in Redis, got err: %v", err)
	}
	if healedMeta.Title != "Fallback Group" {
		t.Errorf("expected healed title 'Fallback Group', got %q", healedMeta.Title)
	}
}

func TestProcessNewMessageInSpeedCache_L1Hit(t *testing.T) {
	setupTestRedis(t)
	ctx := context.Background()

	convID := int64(500)
	senderID := int64(1)
	recipientID := int64(2)
	msgID := int64(9001)

	if err := redis.ConvMeta.SetObject(ctx, convID, lite_models.ConvLiteRequest{
		ID:            convID,
		Title:         "Chat",
		LastMessageID: 9000,
	}); err != nil {
		t.Fatalf("SetObject failed: %v", err)
	}
	if err := redis.ConvParticipants.SAdd(ctx, convID, senderID, recipientID); err != nil {
		t.Fatalf("SAdd failed: %v", err)
	}
	if err := redis.ConvMembers.SetObject(ctx, fmt.Sprintf("%d:%d", convID, recipientID), lite_models.MemberLiteRequest{
		ConversationID: convID,
		UserID:         recipientID,
		UnreadCount:    2,
	}); err != nil {
		t.Fatalf("SetObject member failed: %v", err)
	}

	notified, err := ProcessNewMessageInSpeedCache(ctx, msgID, convID, senderID)
	if err != nil {
		t.Fatalf("ProcessNewMessageInSpeedCache failed: %v", err)
	}
	if len(notified) != 1 || notified[0] != recipientID {
		t.Fatalf("expected notified=[%d], got %v", recipientID, notified)
	}

	var updatedMeta lite_models.ConvLiteRequest
	if err := redis.ConvMeta.GetObject(ctx, convID, &updatedMeta); err != nil {
		t.Fatalf("GetObject meta failed: %v", err)
	}
	if updatedMeta.LastMessageID != msgID {
		t.Errorf("expected LastMessageID=%d, got %d", msgID, updatedMeta.LastMessageID)
	}

	var updatedMember lite_models.MemberLiteRequest
	if err := redis.ConvMembers.GetObject(ctx, fmt.Sprintf("%d:%d", convID, recipientID), &updatedMember); err != nil {
		t.Fatalf("GetObject member failed: %v", err)
	}
	if updatedMember.UnreadCount != 3 {
		t.Errorf("expected UnreadCount=3, got %d", updatedMember.UnreadCount)
	}
}

func TestProcessNewMessageInSpeedCache_L2AndL3Rehydration(t *testing.T) {
	setupTestRedis(t)
	mongoSrv := setupTestMongo(t)
	ctx := context.Background()

	convID := int64(600)
	senderID := int64(10)
	recipientID := int64(20)
	msgID := int64(1005)

	mongoSrv.setDocs("messaging.conversations", bson.M{
		"id":              convID,
		"type":            int32(1),
		"title":           "Mongo Rehydrated Conv",
		"last_message_id": int64(1000),
	})

	now := time.Now().UnixMilli()
	setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
		if strings.Contains(query, "func_get_conversation_participant_ids") {
			return fakeSQLResponse{
				cols: []string{"user_id"},
				rows: [][]driver.Value{{senderID}, {recipientID}},
			}
		}
		return fakeSQLResponse{
			cols: []string{"id", "conversation_id", "user_id", "role", "settings", "joined_at", "unread_count", "frozen_message_id", "last_read_message_id", "created_at", "updated_at"},
			rows: [][]driver.Value{{int64(1), convID, recipientID, int64(0), `{"pinned":2}`, now, int64(5), int64(0), int64(999), now, now}},
		}
	})

	notified, err := ProcessNewMessageInSpeedCache(ctx, msgID, convID, senderID)
	if err != nil {
		t.Fatalf("ProcessNewMessageInSpeedCache failed: %v", err)
	}
	if len(notified) != 1 || notified[0] != recipientID {
		t.Fatalf("expected notified=[%d], got %v", recipientID, notified)
	}

	var meta lite_models.ConvLiteRequest
	if err := redis.ConvMeta.GetObject(ctx, convID, &meta); err != nil {
		t.Fatalf("expected ConvMeta rehydrated in L1, got err: %v", err)
	}
	if meta.Title != "Mongo Rehydrated Conv" || meta.LastMessageID != msgID {
		t.Errorf("unexpected rehydrated ConvMeta: %+v", meta)
	}

	var mem lite_models.MemberLiteRequest
	if err := redis.ConvMembers.GetObject(ctx, fmt.Sprintf("%d:%d", convID, recipientID), &mem); err != nil {
		t.Fatalf("expected ConvMembers rehydrated in L1, got err: %v", err)
	}
	if mem.UnreadCount != 6 || mem.Settings.Pinned != 2 {
		t.Errorf("expected UnreadCount=6 and Pinned=2, got %+v", mem)
	}
}

func TestAddRemoveUpdateAndResetMemberSpeedCache(t *testing.T) {
	setupTestRedis(t)
	ctx := context.Background()

	convID := int64(800)
	userID := int64(88)

	// Seed a community in speed cache to verify member count updates
	if err := StoreCommunityLiteInSpeedCache(ctx, lite_models.CommunityLiteRequest{
		ID:          convID,
		Name:        "Test Community",
		MemberCount: 10,
	}); err != nil {
		t.Fatalf("StoreCommunityLiteInSpeedCache failed: %v", err)
	}

	member := lite_models.MemberLiteRequest{
		ConversationID:    convID,
		UserID:            userID,
		Role:              1,
		LastReadMessageID: 50,
		UnreadCount:       4,
	}

	if err := AddMemberToSpeedCache(ctx, member); err != nil {
		t.Fatalf("AddMemberToSpeedCache failed: %v", err)
	}

	// Verify ResetMemberUnreadCountInSpeedCache
	if err := ResetMemberUnreadCountInSpeedCache(ctx, convID, userID, 120); err != nil {
		t.Fatalf("ResetMemberUnreadCountInSpeedCache failed: %v", err)
	}

	var storedMem lite_models.MemberLiteRequest
	if err := redis.ConvMembers.GetObject(ctx, fmt.Sprintf("%d:%d", convID, userID), &storedMem); err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}
	if storedMem.UnreadCount != 0 || storedMem.LastReadMessageID != 120 {
		t.Errorf("expected UnreadCount=0 and LastReadMessageID=120, got %+v", storedMem)
	}

	// UpdateMemberSpeedCache to inactive role (-1)
	member.Role = -1
	if err := UpdateMemberSpeedCache(ctx, member); err != nil {
		t.Fatalf("UpdateMemberSpeedCache (deactivate) failed: %v", err)
	}
	parts, _ := redis.ConvParticipants.SMembers(ctx, convID)
	if len(parts) != 0 {
		t.Errorf("expected participant removed from SMembers when Role < 0, got %v", parts)
	}

	// UpdateMemberSpeedCache back to active role (0)
	member.Role = 0
	if err := UpdateMemberSpeedCache(ctx, member); err != nil {
		t.Fatalf("UpdateMemberSpeedCache (reactivate) failed: %v", err)
	}
	parts, _ = redis.ConvParticipants.SMembers(ctx, convID)
	if len(parts) != 1 {
		t.Errorf("expected participant re-added to SMembers when Role >= 0, got %v", parts)
	}

	// RemoveMemberFromSpeedCache
	if err := RemoveMemberFromSpeedCache(ctx, convID, userID); err != nil {
		t.Fatalf("RemoveMemberFromSpeedCache failed: %v", err)
	}
	parts, _ = redis.ConvParticipants.SMembers(ctx, convID)
	if len(parts) != 0 {
		t.Errorf("expected participant removed after RemoveMemberFromSpeedCache, got %v", parts)
	}
}

func TestPurgeRehydrateDirectAndInboxHelpers(t *testing.T) {
	setupTestRedis(t)
	ctx := context.Background()

	userOne := int64(11)
	userTwo := int64(22)
	dmConvID := int64(901)

	// RehydrateConversationItemInSpeedCache ignored when paginationOffset >= 100
	RehydrateConversationItemInSpeedCache(ctx,
		conversation_models.ConversationPayload{ID: dmConvID, Type: 0, LastMessageID: 55},
		member_models.MemberPayload{ConversationID: dmConvID, UserID: userOne, Role: 0},
		100,
	)
	var checkMeta lite_models.ConvLiteRequest
	if err := redis.ConvMeta.GetObject(ctx, dmConvID, &checkMeta); err == nil && checkMeta.ID != 0 {
		t.Errorf("expected no rehydration when offset >= 100")
	}

	// Rehydrate when offset < 100
	RehydrateConversationItemInSpeedCache(ctx,
		conversation_models.ConversationPayload{ID: dmConvID, Type: 0, Title: "DM", LastMessageID: 55},
		member_models.MemberPayload{ConversationID: dmConvID, UserID: userOne, Role: 0},
		0,
	)
	_ = redis.ConvParticipants.SAdd(ctx, dmConvID, userTwo)

	// GetDirectConversationCache hit
	foundConvID, err := GetDirectConversationCache(ctx, userOne, userTwo)
	if err != nil {
		t.Fatalf("GetDirectConversationCache failed: %v", err)
	}
	if foundConvID != dmConvID {
		t.Errorf("expected dmConvID=%d, got %d", dmConvID, foundConvID)
	}

	// GetDirectConversationCache miss for non-existent user
	if _, err := GetDirectConversationCache(ctx, userOne, 9999); err == nil {
		t.Errorf("expected error for unknown DM partner, got nil")
	}

	// AddConversationToUserInbox & TouchInboxActivity
	if err := AddConversationToUserInbox(ctx, userOne, 902, 77); err != nil {
		t.Fatalf("AddConversationToUserInbox failed: %v", err)
	}
	ts := TouchInboxActivity(ctx, userOne)
	if ts <= 0 {
		t.Errorf("expected positive timestamp from TouchInboxActivity, got %d", ts)
	}

	// PurgeUserConversations
	if err := PurgeUserConversations(ctx, userOne); err != nil {
		t.Fatalf("PurgeUserConversations failed: %v", err)
	}
	inboxAfter, _ := redis.UserInbox.ZRevRange(ctx, userOne, 0, -1)
	if len(inboxAfter) != 0 {
		t.Errorf("expected empty inbox after PurgeUserConversations, got %v", inboxAfter)
	}
}

func TestSeedMessagingSpeedCache(t *testing.T) {
	setupTestRedis(t)
	ctx := context.Background()

	joinedTime := time.Now().UTC().Truncate(time.Second)
	setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
		if strings.Contains(query, "func_load_active_conversations") {
			return fakeSQLResponse{
				cols: []string{"id", "type", "title", "description", "avatar_id", "last_message_id", "settings", "external_link"},
				rows: [][]driver.Value{
					{int64(301), int64(1), "General", "Desc", int64(5), int64(1001), `{"allow_reactions":true}`, `{"title":"Website","url":"https://example.com"}`},
				},
			}
		}
		return fakeSQLResponse{
			cols: []string{"conversation_id", "user_id", "role", "settings", "unread_count", "frozen_message_id", "last_read_message_id", "last_message_id", "joined_at"},
			rows: [][]driver.Value{
				{int64(301), int64(44), int64(2), `{"pinned":0,"is_muted":1}`, int64(3), int64(0), int64(1000), int64(1001), joinedTime},
			},
		}
	})

	if err := seedMessagingSpeedCache(ctx); err != nil {
		t.Fatalf("seedMessagingSpeedCache failed: %v", err)
	}

	var conv lite_models.ConvLiteRequest
	if err := redis.ConvMeta.GetObject(ctx, int64(301), &conv); err != nil {
		t.Fatalf("expected conversation 301 in Redis, got err: %v", err)
	}
	if conv.Title != "General" || conv.LastMessageID != 1001 {
		t.Errorf("unexpected seeded conversation: %+v", conv)
	}

	var mem lite_models.MemberLiteRequest
	if err := redis.ConvMembers.GetObject(ctx, "301:44", &mem); err != nil {
		t.Fatalf("expected member 301:44 in Redis, got err: %v", err)
	}
	if mem.Role != 2 || mem.UnreadCount != 3 {
		t.Errorf("unexpected seeded member: %+v", mem)
	}
}
