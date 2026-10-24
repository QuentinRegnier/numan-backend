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

func TestFuncLoadAllTags(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		tags, err := FuncLoadAllTags(ctx)
		assert.Nil(t, tags)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{"slug"}

	t.Run("returns_tags_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_load_all_tags")
			return test.NewMockRows(cols, [][]driver.Value{
				{"golang"},
				{"postgres"},
				{"architecture"},
			}), nil
		}, nil)

		tags, err := FuncLoadAllTags(ctx)
		require.NoError(t, err)
		assert.Equal(t, []string{"golang", "postgres", "architecture"}, tags)
	})

	t.Run("handles_empty_tags", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		tags, err := FuncLoadAllTags(ctx)
		require.NoError(t, err)
		assert.NotNil(t, tags)
		assert.Empty(t, tags)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query failed")
		}, nil)

		tags, err := FuncLoadAllTags(ctx)
		assert.Nil(t, tags)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{{"golang"}}, errors.New("iter err")), nil
		}, nil)

		tags, err := FuncLoadAllTags(ctx)
		assert.Nil(t, tags)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
