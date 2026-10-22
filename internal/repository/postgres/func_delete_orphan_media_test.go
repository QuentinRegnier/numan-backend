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

func TestFuncDeleteOrphanMedia(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		results, err := FuncDeleteOrphanMedia(ctx)
		assert.Nil(t, results)
		assertDatabaseError(t, err)
	})

	cols := []string{"id", "storage_path"}

	t.Run("returns_orphan_media_successfully", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_delete_orphan_media")
			return newMockRows(cols, [][]driver.Value{
				{int64(10), "uploads/orphan1.jpg"},
				{int64(20), "uploads/orphan2.jpg"},
			}), nil
		}, nil)

		results, err := FuncDeleteOrphanMedia(ctx)
		require.NoError(t, err)
		require.Len(t, results, 2)
		assert.Equal(t, int64(10), results[0].ID)
		assert.Equal(t, "uploads/orphan1.jpg", results[0].StoragePath)
	})

	t.Run("handles_empty_results", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		results, err := FuncDeleteOrphanMedia(ctx)
		require.NoError(t, err)
		assert.NotNil(t, results)
		assert.Empty(t, results)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db failure")
		}, nil)

		results, err := FuncDeleteOrphanMedia(ctx)
		assert.Nil(t, results)
		assertDatabaseError(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRowsWithErr(cols, [][]driver.Value{{int64(10), "uploads/orphan.jpg"}}, errors.New("iter err")), nil
		}, nil)

		results, err := FuncDeleteOrphanMedia(ctx)
		assert.Nil(t, results)
		assertDatabaseError(t, err)
	})
}
