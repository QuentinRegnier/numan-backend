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

func TestFuncDeleteOrphanReactions(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		ids, err := FuncDeleteOrphanReactions(ctx)
		assert.Nil(t, ids)
		assertDatabaseError(t, err)
	})

	cols := []string{"message_id"}

	t.Run("returns_orphan_message_ids_successfully", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_delete_orphan_reactions")
			return newMockRows(cols, [][]driver.Value{
				{int64(1001)},
				{int64(1002)},
			}), nil
		}, nil)

		ids, err := FuncDeleteOrphanReactions(ctx)
		require.NoError(t, err)
		assert.Equal(t, []int64{1001, 1002}, ids)
	})

	t.Run("handles_empty_results", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		ids, err := FuncDeleteOrphanReactions(ctx)
		require.NoError(t, err)
		assert.NotNil(t, ids)
		assert.Empty(t, ids)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query failed")
		}, nil)

		ids, err := FuncDeleteOrphanReactions(ctx)
		assert.Nil(t, ids)
		assertDatabaseError(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRowsWithErr(cols, [][]driver.Value{{int64(1001)}}, errors.New("stream err")), nil
		}, nil)

		ids, err := FuncDeleteOrphanReactions(ctx)
		assert.Nil(t, ids)
		assertDatabaseError(t, err)
	})
}
