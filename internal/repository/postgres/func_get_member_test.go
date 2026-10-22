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

func TestFuncGetMember(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		m, err := FuncGetMember(ctx, 10, 20)
		assert.Empty(t, m.ID)
		assertDatabaseError(t, err)
	})

	cols := []string{
		"id", "conversation_id", "user_id", "role", "settings", "joined_at",
		"unread_count", "frozen_message_id", "last_read_message_id", "created_at", "updated_at",
	}
	now := time.Now().UnixMilli()

	t.Run("returns_member_successfully", func(t *testing.T) {
		settingsJSON := `{"is_muted":1,"pinned":1}`
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_get_member")
			require.Len(t, args, 2)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(20), args[1].Value)
			return newMockRows(cols, [][]driver.Value{
				{int64(1), int64(10), int64(20), 1, settingsJSON, now, 3, int64(100), int64(150), now, now},
			}), nil
		}, nil)

		m, err := FuncGetMember(ctx, 10, 20)
		require.NoError(t, err)
		assert.Equal(t, int64(1), m.ID)
		assert.Equal(t, int64(10), m.ConversationID)
		assert.Equal(t, int64(20), m.UserID)
		assert.Equal(t, 1, m.Role)
		assert.Equal(t, 3, m.UnreadCount)
		assert.Equal(t, int64(100), m.FrozenMessageID)
		assert.Equal(t, int64(150), m.LastReadMessageID)
		assert.Equal(t, 1, m.Settings.IsMuted)
		assert.Equal(t, 1, m.Settings.Pinned)
	})

	t.Run("handles_member_not_found", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		m, err := FuncGetMember(ctx, 10, 999)
		assert.Empty(t, m.ID)
		assertNotFoundError(t, err, "MEMBER_NOT_FOUND")
	})

	t.Run("handles_invalid_settings_json", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{
				{int64(1), int64(10), int64(20), 1, "not-json", now, 0, nil, nil, now, now},
			}), nil
		}, nil)

		m, err := FuncGetMember(ctx, 10, 20)
		assert.Empty(t, m.ID)
		assertDatabaseError(t, err)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		m, err := FuncGetMember(ctx, 10, 20)
		assert.Empty(t, m.ID)
		assertDatabaseError(t, err)
	})
}
