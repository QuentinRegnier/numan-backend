package cache_service

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoMongo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestGetInboxView(t *testing.T) {
	ctx := context.Background()

	t.Run("Empty inbox returns empty slice", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		res, err := GetInboxView(ctx, 1, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, res)
	})

	t.Run("L1 Hit sorts pinned conversations first then by LastMessageID desc", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		userID := int64(10)

		convs := []lite_models.ConvLiteRequest{
			{ID: 1, Title: "Unpinned Old", LastMessageID: 100},
			{ID: 2, Title: "Pinned #2", LastMessageID: 50},
			{ID: 3, Title: "Pinned #1", LastMessageID: 20},
			{ID: 4, Title: "Unpinned New", LastMessageID: 200},
		}
		pins := map[int64]int{1: -1, 2: 2, 3: 1, 4: -1}

		for _, c := range convs {
			require.NoError(t, redis.UserInbox.ZAddWithCap(ctx, userID, float64(c.LastMessageID), c.ID, 100))
			require.NoError(t, redis.ConvMeta.SetObject(ctx, c.ID, c))
			mem := lite_models.MemberLiteRequest{
				ConversationID: c.ID,
				UserID:         userID,
				Settings:       lite_models.MemberSettingsLite{Pinned: pins[c.ID]},
			}
			require.NoError(t, redis.ConvMembers.SetObject(ctx, fmt.Sprintf("%d:%d", c.ID, userID), mem))
		}

		items, err := GetInboxView(ctx, userID, 10, 0)
		require.NoError(t, err)
		require.Len(t, items, 4)
		assert.Equal(t, int64(3), items[0].Conversation.ID) // Pinned 1
		assert.Equal(t, int64(2), items[1].Conversation.ID) // Pinned 2
		assert.Equal(t, int64(4), items[2].Conversation.ID) // Unpinned LastMessageID 200
		assert.Equal(t, int64(1), items[3].Conversation.ID) // Unpinned LastMessageID 100
	})

	t.Run("L1 Partial Miss and Corrupted Msgpack triggers L3 Fallback and heals L1", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		userID := int64(20)

		require.NoError(t, redis.UserInbox.ZAddWithCap(ctx, userID, 100, 11, 100))
		require.NoError(t, redis.ConvMeta.SetPrimitive(ctx, 11, "bad-msgpack"))
		require.NoError(t, redis.ConvMembers.SetPrimitive(ctx, "11:20", "bad-msgpack"))

		fallbackPayload := []map[string]any{
			{
				"conversation": lite_models.ConvLiteRequest{ID: 11, Title: "Recovered", LastMessageID: 100},
				"member":       lite_models.MemberLiteRequest{ConversationID: 11, UserID: userID, Settings: lite_models.MemberSettingsLite{Pinned: -1}},
			},
		}
		rawJSON, _ := json.Marshal(fallbackPayload)

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows([]string{"func_load_conversation_fallback_json"}, [][]driver.Value{{rawJSON}}), nil
		}, nil)

		items, err := GetInboxView(ctx, userID, 10, 0)
		require.NoError(t, err)
		require.Len(t, items, 1)
		assert.Equal(t, "Recovered", items[0].Conversation.Title)
		time.Sleep(20 * time.Millisecond)
	})

	t.Run("ZRevRange error and MGet error with Postgres error", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		srv.SetFailCmd("ZREVRANGE", errors.New("zrevrange err"))
		_, err := GetInboxView(ctx, 1, 10, 0)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)

		srv.SetFailCmd("ZREVRANGE", nil)
		require.NoError(t, redis.UserInbox.ZAddWithCap(ctx, 1, 100, 99, 100))
		srv.SetFailCmd("MGET", errors.New("mget err"))

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("pg fallback err")
		}, nil)

		items, err := GetInboxView(ctx, 1, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, items)
	})
}

func TestProcessNewMessageInSpeedCache(t *testing.T) {
	ctx := context.Background()

	t.Run("L1 Hit updates conversation, inbox, and recipient unread count", func(t *testing.T) {
		_ = test.SetupTestRedis(t)

		convID := int64(50)
		senderID := int64(1)
		recipientID := int64(2)

		require.NoError(t, redis.ConvMeta.SetObject(ctx, convID, lite_models.ConvLiteRequest{ID: convID, LastMessageID: 10}))
		require.NoError(t, redis.ConvParticipants.SAdd(ctx, convID, senderID, recipientID))
		require.NoError(t, redis.ConvMembers.SetObject(ctx, "50:2", lite_models.MemberLiteRequest{
			ConversationID: convID, UserID: recipientID, UnreadCount: 3,
		}))

		notified, err := ProcessNewMessageInSpeedCache(ctx, 999, convID, senderID)
		require.NoError(t, err)
		assert.Equal(t, []int64{recipientID}, notified)

		var mem lite_models.MemberLiteRequest
		require.NoError(t, redis.ConvMembers.GetObject(ctx, "50:2", &mem))
		assert.Equal(t, 4, mem.UnreadCount)
	})

	t.Run("L1 Miss heals from L2 Mongo and L3 Postgres", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
		restore := test.ResetCollections()
		defer restore()

		mt.Run("mongo hit and pg participants+member hit", func(mt *mtest.T) {
			repoMongo.Conversations = test.NewMockCollection(mt, "messaging.conversations", nil)
			repoMongo.Members = test.NewMockCollection(mt, "messaging.members", nil)
			mt.AddMockResponses(
				mtest.CreateCursorResponse(0, "numan_mongo.messaging.conversations", mtest.FirstBatch, bson.D{
					{Key: "id", Value: int64(60)},
					{Key: "type", Value: 1},
					{Key: "title", Value: "Group"},
				}),
				mtest.CreateCursorResponse(0, "numan_mongo.messaging.members", mtest.FirstBatch),
			)

			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "func_get_conversation_participant_ids") {
					return test.NewMockRows([]string{"user_id"}, [][]driver.Value{{int64(1)}, {int64(2)}}), nil
				}
				return test.NewMockRows(
					[]string{"id", "conversation_id", "user_id", "role", "settings", "joined_at", "unread_count", "frozen_message_id", "last_read_message_id", "created_at", "updated_at"},
					[][]driver.Value{{int64(10), int64(60), int64(2), 1, `{}`, int64(1000), 5, int64(0), int64(0), int64(1000), int64(1000)}},
				), nil
			}, nil)

			notified, err := ProcessNewMessageInSpeedCache(ctx, 777, 60, 1)
			require.NoError(t, err)
			assert.Equal(t, []int64{2}, notified)
		})
	})

	t.Run("Error Paths in ProcessNewMessageInSpeedCache", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
		restore := test.ResetCollections()
		defer restore()

		mt.Run("ConvMeta SetObject failure on L1 hit", func(mt *mtest.T) {
			require.NoError(t, redis.ConvMeta.SetObject(ctx, 70, lite_models.ConvLiteRequest{ID: 70}))
			srv.SetFailCmd("SET", errors.New("set err"))
			_, err := ProcessNewMessageInSpeedCache(ctx, 100, 70, 1)
			var appErr *numan_error.AppError
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
			srv.SetFailCmd("SET", nil)
		})

		mt.Run("L2 and L3 errors logged and Eval failure", func(mt *mtest.T) {
			repoMongo.Conversations = test.NewMockCollection(mt, "messaging.conversations", nil)
			mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 1, Message: "mongo err"}))

			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				return nil, errors.New("pg err")
			}, nil)

			notified, err := ProcessNewMessageInSpeedCache(ctx, 100, 71, 1)
			require.NoError(t, err)
			assert.Empty(t, notified)
		})
	})
}

func TestMemberAndInboxOperations(t *testing.T) {
	ctx := context.Background()

	t.Run("AddMemberToSpeedCache, UpdateMemberSpeedCache, ResetMemberUnreadCount, RemoveMemberFromSpeedCache", func(t *testing.T) {
		_ = test.SetupTestRedis(t)

		comm := lite_models.CommunityLiteRequest{ID: 80, Name: "Comm", MemberCount: 1}
		require.NoError(t, StoreCommunityLiteInSpeedCache(ctx, comm))

		mem := lite_models.MemberLiteRequest{ConversationID: 80, UserID: 5, Role: 1, UnreadCount: 3, LastReadMessageID: 10}
		require.NoError(t, AddMemberToSpeedCache(ctx, mem))

		// Reset unread count
		require.NoError(t, ResetMemberUnreadCountInSpeedCache(ctx, 80, 5, 25))
		var loaded lite_models.MemberLiteRequest
		require.NoError(t, redis.ConvMembers.GetObject(ctx, "80:5", &loaded))
		assert.Equal(t, 0, loaded.UnreadCount)
		assert.Equal(t, int64(25), loaded.LastReadMessageID)

		// Update member to inactive (Role < 0)
		mem.Role = -1
		require.NoError(t, UpdateMemberSpeedCache(ctx, mem))
		// Update member back to active (Role >= 0)
		mem.Role = 0
		require.NoError(t, UpdateMemberSpeedCache(ctx, mem))

		// Add conversation to user inbox & touch activity
		require.NoError(t, AddConversationToUserInbox(ctx, 5, 80, 25))
		assert.Greater(t, TouchInboxActivity(ctx, 5), int64(0))

		// Remove member
		require.NoError(t, RemoveMemberFromSpeedCache(ctx, 80, 5))

		// Purge conversations
		require.NoError(t, PurgeUserConversations(ctx, 5))
	})

	t.Run("RehydrateConversationItemInSpeedCache", func(t *testing.T) {
		_ = test.SetupTestRedis(t)

		conv := conversation_models.ConversationPayload{ID: 90, Title: "Chat", LastMessageID: 555}
		mem := member_models.MemberPayload{ConversationID: 90, UserID: 7, Role: 1}

		// offset >= 100 is ignored
		RehydrateConversationItemInSpeedCache(ctx, conv, mem, 100)
		var c lite_models.ConvLiteRequest
		assert.Error(t, redis.ConvMeta.GetObject(ctx, 90, &c))

		// offset < 100 rehydrates
		RehydrateConversationItemInSpeedCache(ctx, conv, mem, 0)
		require.NoError(t, redis.ConvMeta.GetObject(ctx, 90, &c))
		assert.Equal(t, "Chat", c.Title)
	})

	t.Run("GetDirectConversationCache", func(t *testing.T) {
		srv := test.SetupTestRedis(t)

		// Empty inbox -> NotFound
		_, err := GetDirectConversationCache(ctx, 1, 2)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)

		// Populate DM (Type 0)
		require.NoError(t, redis.UserInbox.ZAddWithCap(ctx, 1, 100, 500, 100))
		require.NoError(t, redis.ConvMeta.SetObject(ctx, 500, lite_models.ConvLiteRequest{ID: 500, Type: 0}))
		require.NoError(t, redis.ConvParticipants.SAdd(ctx, 500, 1, 2))

		convID, err := GetDirectConversationCache(ctx, 1, 2)
		require.NoError(t, err)
		assert.Equal(t, int64(500), convID)

		// User 3 not in DM -> NotFound
		_, err = GetDirectConversationCache(ctx, 1, 3)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)

		// Redis errors
		srv.SetFailCmd("MGET", errors.New("mget err"))
		_, err = GetDirectConversationCache(ctx, 1, 2)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("MGET", nil)

		srv.SetFailCmd("ZREVRANGE", errors.New("zrev err"))
		_, err = GetDirectConversationCache(ctx, 1, 2)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
	})

	t.Run("seedMessagingSpeedCache", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		now := time.Now()

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "func_load_active_conversations") {
				return test.NewMockRows(
					[]string{"id", "type", "title", "description", "avatar_id", "last_message_id", "settings", "external_link"},
					[][]driver.Value{{int64(1), 0, "DM", "", int64(0), int64(10), `{}`, nil}},
				), nil
			}
			return test.NewMockRows(
				[]string{"conversation_id", "user_id", "role", "settings", "unread_count", "frozen_message_id", "last_read_message_id", "last_message_id", "joined_at"},
				[][]driver.Value{{int64(1), int64(2), 0, `{}`, 0, int64(0), int64(10), int64(10), now}},
			), nil
		}, nil)

		require.NoError(t, seedMessagingSpeedCache(ctx))
	})

	t.Run("Error Paths for Member & Inbox Operations", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		var appErr *numan_error.AppError

		mem := lite_models.MemberLiteRequest{ConversationID: 88, UserID: 9, Role: 1}

		srv.SetFailCmd("SADD", errors.New("sadd err"))
		require.ErrorAs(t, AddMemberToSpeedCache(ctx, mem), &appErr)
		srv.SetFailCmd("SADD", nil)

		srv.SetFailCmd("SET", errors.New("set err"))
		require.ErrorAs(t, AddMemberToSpeedCache(ctx, mem), &appErr)
		assert.Equal(t, int64(-1), TouchInboxActivity(ctx, 9))
		require.ErrorAs(t, UpdateMemberSpeedCache(ctx, mem), &appErr)
		srv.SetFailCmd("SET", nil)

		srv.SetFailCmd("SREM", errors.New("srem err"))
		require.ErrorAs(t, RemoveMemberFromSpeedCache(ctx, 88, 9), &appErr)
		mem.Role = -1
		require.ErrorAs(t, UpdateMemberSpeedCache(ctx, mem), &appErr)
		srv.SetFailCmd("SREM", nil)

		srv.SetFailCmd("DEL", errors.New("del err"))
		require.ErrorAs(t, RemoveMemberFromSpeedCache(ctx, 88, 9), &appErr)
		require.ErrorAs(t, PurgeUserConversations(ctx, 9), &appErr)
		srv.SetFailCmd("DEL", nil)

		srv.SetFailCmd("ZREM", errors.New("zrem err"))
		require.ErrorAs(t, RemoveMemberFromSpeedCache(ctx, 88, 9), &appErr)
		srv.SetFailCmd("ZREM", nil)

		srv.SetFailCmd("EVAL", errors.New("eval err"))
		require.ErrorAs(t, AddConversationToUserInbox(ctx, 9, 88, 1), &appErr)
		srv.SetFailCmd("EVAL", nil)

		srv.SetFailCmd("GET", errors.New("get err"))
		require.NoError(t, ResetMemberUnreadCountInSpeedCache(ctx, 88, 9, 1))
		srv.SetFailCmd("GET", nil)
	})
}
