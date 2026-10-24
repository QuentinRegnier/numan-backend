package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncLoadPostsForGraphSeeding(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		seeds, err := FuncLoadPostsForGraphSeeding(ctx)
		assert.Nil(t, seeds)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{"id", "hashtags", "indirect_hashtags", "created_at"}
	now := time.Now()

	t.Run("returns_graph_seeds_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_load_posts_for_graph_seeding")
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(101), "{golang,postgres}", "{backend}", now},
				{int64(102), "{cloud}", "{}", now},
			}), nil
		}, nil)

		seeds, err := FuncLoadPostsForGraphSeeding(ctx)
		require.NoError(t, err)
		require.Len(t, seeds, 2)
		assert.Equal(t, int64(101), seeds[0].ID)
		assert.Equal(t, []string{"golang", "postgres"}, seeds[0].Hashtags)
		assert.Equal(t, []string{"backend"}, seeds[0].IndirectHashtags)
		assert.Equal(t, int64(102), seeds[1].ID)
	})

	t.Run("handles_empty_seeds", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		seeds, err := FuncLoadPostsForGraphSeeding(ctx)
		require.NoError(t, err)
		assert.NotNil(t, seeds)
		assert.Empty(t, seeds)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		seeds, err := FuncLoadPostsForGraphSeeding(ctx)
		assert.Nil(t, seeds)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{
				{int64(101), "{}", "{}", now},
			}, errors.New("iter err")), nil
		}, nil)

		seeds, err := FuncLoadPostsForGraphSeeding(ctx)
		assert.Nil(t, seeds)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
