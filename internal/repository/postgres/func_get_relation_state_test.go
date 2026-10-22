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

func TestFuncGetRelationState(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		state, err := FuncGetRelationState(ctx, 10, 20)
		assert.Equal(t, 0, state)
		assertDatabaseError(t, err)
	})

	t.Run("returns_the_database_value", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_get_relation_state")
			require.Len(t, args, 2)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(20), args[1].Value)
			return newMockRows([]string{"state"}, [][]driver.Value{{int64(2)}}), nil
		}, nil)

		state, err := FuncGetRelationState(ctx, 10, 20)
		require.NoError(t, err)
		assert.Equal(t, 2, state)
	})

	t.Run("handles_no_rows_as_neutral_state", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows([]string{"state"}, [][]driver.Value{}), nil
		}, nil)

		state, err := FuncGetRelationState(ctx, 10, 20)
		require.NoError(t, err)
		assert.Equal(t, 0, state)
	})

	t.Run("returns_structured_error_on_query_failure", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("connection reset by peer")
		}, nil)

		state, err := FuncGetRelationState(ctx, 10, 20)
		assert.Equal(t, 0, state)
		assertDatabaseError(t, err)
	})
}
