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

func TestFuncGetPinnedIndices(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		indices, err := FuncGetPinnedIndices(ctx, 42)
		assert.Nil(t, indices)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{"pinned"}

	t.Run("returns_pinned_indices_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_get_pinned_indices")
			require.Len(t, args, 1)
			assert.Equal(t, int64(42), args[0].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(0)},
				{int64(1)},
				{int64(3)},
			}), nil
		}, nil)

		indices, err := FuncGetPinnedIndices(ctx, 42)
		require.NoError(t, err)
		assert.Equal(t, []int{0, 1, 3}, indices)
	})

	t.Run("handles_empty_results", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		indices, err := FuncGetPinnedIndices(ctx, 42)
		require.NoError(t, err)
		assert.NotNil(t, indices)
		assert.Empty(t, indices)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		indices, err := FuncGetPinnedIndices(ctx, 42)
		assert.Nil(t, indices)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{{int64(0)}}, errors.New("iter err")), nil
		}, nil)

		indices, err := FuncGetPinnedIndices(ctx, 42)
		assert.Nil(t, indices)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
