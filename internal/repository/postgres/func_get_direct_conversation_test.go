package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncGetDirectConversation(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		c, err := FuncGetDirectConversation(ctx, 10, 20)
		assert.Empty(t, c.ID)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{"json_bytes"}

	t.Run("returns_unmarshaled_direct_conversation_successfully", func(t *testing.T) {
		jsonBlob := []byte(`{"id":555,"type":0,"title":"Direct Chat"}`)
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_get_direct_conversation_json")
			require.Len(t, args, 2)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(20), args[1].Value)
			return test.NewMockRows(cols, [][]driver.Value{{jsonBlob}}), nil
		}, nil)

		c, err := FuncGetDirectConversation(ctx, 10, 20)
		require.NoError(t, err)
		assert.Equal(t, int64(555), c.ID)
		assert.Equal(t, 0, c.Type)
		assert.Equal(t, "Direct Chat", c.Title)
	})

	t.Run("handles_no_rows_not_found", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		c, err := FuncGetDirectConversation(ctx, 10, 20)
		assert.Empty(t, c.ID)
		test.AssertNotFoundError_Postgres(t, err, "CONV_NOT_FOUND")
	})

	t.Run("handles_null_json_bytes_not_found", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{{nil}}), nil
		}, nil)

		c, err := FuncGetDirectConversation(ctx, 10, 20)
		assert.Empty(t, c.ID)
		test.AssertNotFoundError_Postgres(t, err, "CONV_NOT_FOUND")
	})

	t.Run("handles_invalid_json_deserialization_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{{[]byte("{invalid-json")}}), nil
		}, nil)

		c, err := FuncGetDirectConversation(ctx, 10, 20)
		assert.Empty(t, c.ID)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query failed")
		}, nil)

		c, err := FuncGetDirectConversation(ctx, 10, 20)
		assert.Empty(t, c.ID)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
