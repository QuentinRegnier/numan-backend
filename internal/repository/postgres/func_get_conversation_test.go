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

func TestFuncGetConversation(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		c, err := FuncGetConversation(ctx, 123)
		assert.Empty(t, c.ID)
		assertDatabaseError(t, err)
	})

	cols := []string{"json_bytes"}

	t.Run("returns_unmarshaled_conversation_successfully", func(t *testing.T) {
		jsonBlob := []byte(`{"id":123,"type":1,"title":"General Chat"}`)
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_get_conversation_json")
			require.Len(t, args, 1)
			assert.Equal(t, int64(123), args[0].Value)
			return newMockRows(cols, [][]driver.Value{{jsonBlob}}), nil
		}, nil)

		c, err := FuncGetConversation(ctx, 123)
		require.NoError(t, err)
		assert.Equal(t, int64(123), c.ID)
		assert.Equal(t, 1, c.Type)
		assert.Equal(t, "General Chat", c.Title)
	})

	t.Run("handles_no_rows_not_found", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		c, err := FuncGetConversation(ctx, 999)
		assert.Empty(t, c.ID)
		assertNotFoundError(t, err, "CONV_NOT_FOUND")
	})

	t.Run("handles_null_json_bytes_not_found", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{{nil}}), nil
		}, nil)

		c, err := FuncGetConversation(ctx, 999)
		assert.Empty(t, c.ID)
		assertNotFoundError(t, err, "CONV_NOT_FOUND")
	})

	t.Run("handles_invalid_json_deserialization_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{{[]byte("{invalid-json")}}), nil
		}, nil)

		c, err := FuncGetConversation(ctx, 123)
		assert.Empty(t, c.ID)
		assertDatabaseError(t, err)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		c, err := FuncGetConversation(ctx, 123)
		assert.Empty(t, c.ID)
		assertDatabaseError(t, err)
	})
}
