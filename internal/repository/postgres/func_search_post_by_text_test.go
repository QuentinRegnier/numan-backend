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

func TestFuncSearchPostIDsByText(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		ids, err := FuncSearchPostIDsByText(ctx, "keyword", 0, 0, 10)
		assert.Nil(t, ids)
		assertDatabaseError(t, err)
	})

	cols := []string{"id"}

	t.Run("returns_search_ids_by_text_successfully", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_search_post_ids_by_text")
			require.Len(t, args, 4)
			assert.Equal(t, "golang concurrency", args[0].Value)
			assert.Equal(t, int64(0), args[1].Value)
			assert.Equal(t, int64(0), args[2].Value)
			assert.Equal(t, int64(10), args[3].Value)
			return newMockRows(cols, [][]driver.Value{
				{int64(701)},
				{int64(702)},
			}), nil
		}, nil)

		ids, err := FuncSearchPostIDsByText(ctx, "golang concurrency", 0, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []int64{701, 702}, ids)
	})

	t.Run("handles_empty_search_results", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		ids, err := FuncSearchPostIDsByText(ctx, "nonexistent", 0, 0, 10)
		require.NoError(t, err)
		assert.NotNil(t, ids)
		assert.Empty(t, ids)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query error")
		}, nil)

		ids, err := FuncSearchPostIDsByText(ctx, "keyword", 0, 0, 10)
		assert.Nil(t, ids)
		assertDatabaseError(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRowsWithErr(cols, [][]driver.Value{{int64(701)}}, errors.New("stream err")), nil
		}, nil)

		ids, err := FuncSearchPostIDsByText(ctx, "keyword", 0, 0, 10)
		assert.Nil(t, ids)
		assertDatabaseError(t, err)
	})
}
