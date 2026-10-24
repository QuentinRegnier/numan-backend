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

func TestFuncSearchPostIDsByTag(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		ids, err := FuncSearchPostIDsByTag(ctx, "science", 0, 0, 10)
		assert.Nil(t, ids)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{"id"}

	t.Run("returns_search_ids_by_tag_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_search_post_ids_by_tag")
			require.Len(t, args, 4)
			assert.Equal(t, "science", args[0].Value)
			assert.Equal(t, int64(1), args[1].Value)
			assert.Equal(t, int64(0), args[2].Value)
			assert.Equal(t, int64(10), args[3].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(501)},
				{int64(502)},
			}), nil
		}, nil)

		ids, err := FuncSearchPostIDsByTag(ctx, "science", 1, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []int64{501, 502}, ids)
	})

	t.Run("handles_empty_search_results", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		ids, err := FuncSearchPostIDsByTag(ctx, "nothing", 0, 0, 10)
		require.NoError(t, err)
		assert.NotNil(t, ids)
		assert.Empty(t, ids)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		ids, err := FuncSearchPostIDsByTag(ctx, "science", 0, 0, 10)
		assert.Nil(t, ids)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{{int64(501)}}, errors.New("iter err")), nil
		}, nil)

		ids, err := FuncSearchPostIDsByTag(ctx, "science", 0, 0, 10)
		assert.Nil(t, ids)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
