package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncLoadConversationFallback(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		res, err := FuncLoadConversationFallback(ctx, 10, []int64{1, 2})
		assert.Nil(t, res)
		assertDatabaseError(t, err)
	})

	cols := []string{"json_bytes"}

	t.Run("returns_unmarshaled_fallback_results_successfully", func(t *testing.T) {
		jsonBlob := []byte(`[{"conversation":{"id":100,"type":1,"title":"Inbox 1"},"member":{"conversation_id":100,"user_id":10,"role":1}}]`)
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_load_conversation_fallback_json")
			require.Len(t, args, 2)
			assert.Equal(t, int64(10), args[0].Value)
			return newMockRows(cols, [][]driver.Value{{jsonBlob}}), nil
		}, nil)

		res, err := FuncLoadConversationFallback(ctx, 10, []int64{100})
		require.NoError(t, err)
		require.Len(t, res, 1)
		assert.Equal(t, int64(100), res[0].Conversation.ID)
		assert.Equal(t, "Inbox 1", res[0].Conversation.Title)
		assert.Equal(t, int64(10), res[0].Member.UserID)
	})

	t.Run("handles_no_rows_returning_empty_slice", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		res, err := FuncLoadConversationFallback(ctx, 10, []int64{100})
		require.NoError(t, err)
		assert.NotNil(t, res)
		assert.Empty(t, res)
	})

	t.Run("handles_null_bytes_returning_empty_slice", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{{nil}}), nil
		}, nil)

		res, err := FuncLoadConversationFallback(ctx, 10, []int64{100})
		require.NoError(t, err)
		assert.NotNil(t, res)
		assert.Empty(t, res)
	})

	t.Run("handles_json_deserialization_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{{[]byte("{corrupt-json")}}), nil
		}, nil)

		res, err := FuncLoadConversationFallback(ctx, 10, []int64{100})
		assert.Nil(t, res)
		assertDatabaseError(t, err)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		res, err := FuncLoadConversationFallback(ctx, 10, []int64{100})
		assert.Nil(t, res)
		assertDatabaseError(t, err)
	})
}
