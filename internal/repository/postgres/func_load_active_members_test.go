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

func TestFuncLoadActiveMembers(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		members, err := FuncLoadActiveMembers(ctx)
		assert.Nil(t, members)
		assertDatabaseError(t, err)
	})

	cols := []string{
		"conversation_id", "user_id", "role", "settings", "unread_count",
		"frozen_message_id", "last_read_message_id", "last_message_id", "joined_at",
	}
	now := time.Now()

	t.Run("returns_active_members_successfully", func(t *testing.T) {
		settingsJSON := `{"is_muted":1,"pinned":2}`
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_load_active_members")
			return newMockRows(cols, [][]driver.Value{
				{int64(100), int64(200), 1, settingsJSON, 4, int64(10), int64(15), int64(25), now},
			}), nil
		}, nil)

		members, err := FuncLoadActiveMembers(ctx)
		require.NoError(t, err)
		require.Len(t, members, 1)
		assert.Equal(t, int64(100), members[0].Member.ConversationID)
		assert.Equal(t, int64(200), members[0].Member.UserID)
		assert.Equal(t, 1, members[0].Member.Role)
		assert.Equal(t, 1, members[0].Member.Settings.IsMuted)
		assert.Equal(t, 2, members[0].Member.Settings.Pinned)
		assert.Equal(t, int64(10), members[0].Member.FrozenMessageID)
		assert.Equal(t, int64(15), members[0].Member.LastReadMessageID)
		assert.True(t, members[0].LastMessageID.Valid)
		assert.Equal(t, int64(25), members[0].LastMessageID.Int64)
	})

	t.Run("handles_invalid_settings_json", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{
				{int64(100), int64(200), 1, "corrupt-json", 0, nil, nil, nil, now},
			}), nil
		}, nil)

		members, err := FuncLoadActiveMembers(ctx)
		assert.Nil(t, members)
		assertDatabaseError(t, err)
	})

	t.Run("handles_empty_results", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		members, err := FuncLoadActiveMembers(ctx)
		require.NoError(t, err)
		assert.NotNil(t, members)
		assert.Empty(t, members)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		members, err := FuncLoadActiveMembers(ctx)
		assert.Nil(t, members)
		assertDatabaseError(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRowsWithErr(cols, [][]driver.Value{
				{int64(100), int64(200), 1, "{}", 0, nil, nil, nil, now},
			}, errors.New("iter err")), nil
		}, nil)

		members, err := FuncLoadActiveMembers(ctx)
		assert.Nil(t, members)
		assertDatabaseError(t, err)
	})
}
