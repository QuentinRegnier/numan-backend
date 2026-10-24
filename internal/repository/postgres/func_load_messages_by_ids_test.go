package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncLoadMessagesByIDs(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		messages, err := FuncLoadMessagesByIDs(ctx, []int64{1, 2})
		assert.Nil(t, messages)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{
		"id", "conversation_id", "sender_id", "message_type",
		"visibility", "content", "attachments", "allow_new_reactions", "created_at", "updated_at",
	}
	now := time.Now().UnixMilli()

	t.Run("returns_messages_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_load_messages_by_ids")
			require.Len(t, args, 1)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(1), int64(10), int64(20), 0, 1, "Hello", nil, true, now, now},
			}), nil
		}, nil)

		messages, err := FuncLoadMessagesByIDs(ctx, []int64{1})
		require.NoError(t, err)
		require.Len(t, messages, 1)
		assert.Equal(t, int64(1), messages[0].ID)
		assert.Equal(t, int64(10), messages[0].ConversationID)
		assert.Equal(t, "Hello", messages[0].Content)
		assert.True(t, messages[0].AllowNewReactions)
	})

	t.Run("handles_empty_messages", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		messages, err := FuncLoadMessagesByIDs(ctx, []int64{999})
		require.NoError(t, err)
		assert.NotNil(t, messages)
		assert.Empty(t, messages)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db failure")
		}, nil)

		messages, err := FuncLoadMessagesByIDs(ctx, []int64{1})
		assert.Nil(t, messages)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{
				{int64(1), int64(10), int64(20), 0, 1, "Hello", nil, true, now, now},
			}, errors.New("iter err")), nil
		}, nil)

		messages, err := FuncLoadMessagesByIDs(ctx, []int64{1})
		assert.Nil(t, messages)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
