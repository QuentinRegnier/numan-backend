package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncLoadConversationPaginated(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		results, err := FuncLoadConversationPaginated(ctx, 10, 10, 0)
		assert.Nil(t, results)
		assertDatabaseError(t, err)
	})

	cols := []string{
		"conv_id", "conv_type", "conv_title", "conv_description", "conv_avatar_id", "conv_last_msg_id",
		"conv_state", "conv_settings", "external_link", "conv_created", "conv_updated",
		"mem_id", "mem_conv_id", "mem_user_id", "mem_role", "mem_settings", "mem_joined",
		"mem_unread", "mem_frozen_id", "mem_last_read_message_id", "mem_created", "mem_updated",
	}
	now := time.Now().UnixMilli()

	t.Run("returns_conversations_paginated_successfully", func(t *testing.T) {
		convSettings := `{"theme":"dark"}`
		extLink := `{"website":"https://numan.app"}`
		memSettings := `{"pinned":1}`

		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_load_conversation_paginated")
			require.Len(t, args, 3)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(10), args[1].Value)
			assert.Equal(t, int64(0), args[2].Value)
			return newMockRows(cols, [][]driver.Value{
				{
					int64(100), 1, "Conv Title", "Conv Desc", int64(5), int64(50),
					0, convSettings, extLink, now, now,
					int64(1), int64(100), int64(10), 1, memSettings, now,
					2, int64(20), int64(30), now, now,
				},
			}), nil
		}, nil)

		results, err := FuncLoadConversationPaginated(ctx, 10, 10, 0)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, int64(100), results[0].Conversation.ID)
		assert.Equal(t, "Conv Title", results[0].Conversation.Title)
		assert.Equal(t, int64(1), results[0].Member.ID)
		assert.Equal(t, int64(10), results[0].Member.UserID)
		assert.Equal(t, 2, results[0].Member.UnreadCount)
		assert.Equal(t, int64(20), results[0].Member.FrozenMessageID)
		assert.Equal(t, int64(30), results[0].Member.LastReadMessageID)
	})

	t.Run("handles_invalid_conv_settings_json", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{
				{
					int64(100), 1, "Title", nil, nil, nil,
					0, "invalid-json", nil, now, now,
					int64(1), int64(100), int64(10), 1, "{}", now,
					0, nil, nil, now, now,
				},
			}), nil
		}, nil)

		results, err := FuncLoadConversationPaginated(ctx, 10, 10, 0)
		assert.Nil(t, results)
		assertDatabaseError(t, err)
	})

	t.Run("handles_invalid_external_link_json", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{
				{
					int64(100), 1, "Title", nil, nil, nil,
					0, "{}", "invalid-link-json", now, now,
					int64(1), int64(100), int64(10), 1, "{}", now,
					0, nil, nil, now, now,
				},
			}), nil
		}, nil)

		results, err := FuncLoadConversationPaginated(ctx, 10, 10, 0)
		assert.Nil(t, results)
		assertDatabaseError(t, err)
	})

	t.Run("handles_invalid_member_settings_json", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{
				{
					int64(100), 1, "Title", nil, nil, nil,
					0, "{}", nil, now, now,
					int64(1), int64(100), int64(10), 1, "invalid-mem-settings", now,
					0, nil, nil, now, now,
				},
			}), nil
		}, nil)

		results, err := FuncLoadConversationPaginated(ctx, 10, 10, 0)
		assert.Nil(t, results)
		assertDatabaseError(t, err)
	})

	t.Run("handles_empty_results", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		results, err := FuncLoadConversationPaginated(ctx, 10, 10, 0)
		require.NoError(t, err)
		assert.NotNil(t, results)
		assert.Empty(t, results)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query failed")
		}, nil)

		results, err := FuncLoadConversationPaginated(ctx, 10, 10, 0)
		assert.Nil(t, results)
		assertDatabaseError(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRowsWithErr(cols, [][]driver.Value{
				{
					int64(100), 1, "Title", nil, nil, nil,
					0, "{}", nil, now, now,
					int64(1), int64(100), int64(10), 1, "{}", now,
					0, nil, nil, now, now,
				},
			}, errors.New("iter err")), nil
		}, nil)

		results, err := FuncLoadConversationPaginated(ctx, 10, 10, 0)
		assert.Nil(t, results)
		assertDatabaseError(t, err)
	})
}
