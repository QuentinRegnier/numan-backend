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

func TestFuncLoadAddableRelations(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		rels, err := FuncLoadAddableRelations(ctx, 10)
		assert.Nil(t, rels)
		assertDatabaseError(t, err)
	})

	cols := []string{"target_id", "state"}

	t.Run("returns_addable_relations_successfully", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_load_addable_relations")
			require.Len(t, args, 1)
			assert.Equal(t, int64(10), args[0].Value)
			return newMockRows(cols, [][]driver.Value{
				{int64(101), 1},
				{int64(102), 2},
			}), nil
		}, nil)

		rels, err := FuncLoadAddableRelations(ctx, 10)
		require.NoError(t, err)
		require.Len(t, rels, 2)
		assert.Equal(t, int64(101), rels[0].TargetID)
		assert.Equal(t, 1, rels[0].State)
		assert.Equal(t, int64(102), rels[1].TargetID)
	})

	t.Run("handles_empty_relations", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		rels, err := FuncLoadAddableRelations(ctx, 10)
		require.NoError(t, err)
		assert.NotNil(t, rels)
		assert.Empty(t, rels)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		rels, err := FuncLoadAddableRelations(ctx, 10)
		assert.Nil(t, rels)
		assertDatabaseError(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRowsWithErr(cols, [][]driver.Value{{int64(101), 1}}, errors.New("iter err")), nil
		}, nil)

		rels, err := FuncLoadAddableRelations(ctx, 10)
		assert.Nil(t, rels)
		assertDatabaseError(t, err)
	})
}
