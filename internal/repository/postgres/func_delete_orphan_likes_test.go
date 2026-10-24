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

func TestFuncDeleteOrphanLikes(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		targets, err := FuncDeleteOrphanLikes(ctx)
		assert.Nil(t, targets)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{"target_type", "target_id"}

	t.Run("returns_orphan_targets_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_delete_orphan_likes")
			return test.NewMockRows(cols, [][]driver.Value{
				{1, int64(100)},
				{2, int64(200)},
			}), nil
		}, nil)

		targets, err := FuncDeleteOrphanLikes(ctx)
		require.NoError(t, err)
		require.Len(t, targets, 2)
		assert.Equal(t, 1, targets[0].TargetType)
		assert.Equal(t, int64(100), targets[0].TargetID)
	})

	t.Run("handles_empty_results", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		targets, err := FuncDeleteOrphanLikes(ctx)
		require.NoError(t, err)
		assert.NotNil(t, targets)
		assert.Empty(t, targets)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query failed")
		}, nil)

		targets, err := FuncDeleteOrphanLikes(ctx)
		assert.Nil(t, targets)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{{1, int64(100)}}, errors.New("iter err")), nil
		}, nil)

		targets, err := FuncDeleteOrphanLikes(ctx)
		assert.Nil(t, targets)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
