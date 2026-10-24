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

func TestFuncDeleteOrphanSaved(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		ids, err := FuncDeleteOrphanSaved(ctx)
		assert.Nil(t, ids)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{"post_id"}

	t.Run("returns_orphan_post_ids_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_delete_orphan_saved")
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(5001)},
				{int64(5002)},
			}), nil
		}, nil)

		ids, err := FuncDeleteOrphanSaved(ctx)
		require.NoError(t, err)
		assert.Equal(t, []int64{5001, 5002}, ids)
	})

	t.Run("handles_empty_results", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		ids, err := FuncDeleteOrphanSaved(ctx)
		require.NoError(t, err)
		assert.NotNil(t, ids)
		assert.Empty(t, ids)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query failed")
		}, nil)

		ids, err := FuncDeleteOrphanSaved(ctx)
		assert.Nil(t, ids)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{{int64(5001)}}, errors.New("stream err")), nil
		}, nil)

		ids, err := FuncDeleteOrphanSaved(ctx)
		assert.Nil(t, ids)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
