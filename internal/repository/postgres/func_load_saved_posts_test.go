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

func TestFuncLoadSavedPostsKeyset(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		saveds, err := FuncLoadSavedPostsKeyset(ctx, 10, nil, nil, 10)
		assert.Nil(t, saveds)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{"id", "user_id", "post_id", "created_at"}

	t.Run("returns_saved_posts_successfully", func(t *testing.T) {
		beforeCreated := int64(1700000000)
		beforeID := int64(50)
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_load_saved_posts_keyset")
			require.Len(t, args, 4)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(1700000000), args[1].Value)
			assert.Equal(t, int64(50), args[2].Value)
			assert.Equal(t, int64(10), args[3].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(1), int64(10), int64(101), int64(1699999999)},
			}), nil
		}, nil)

		saveds, err := FuncLoadSavedPostsKeyset(ctx, 10, &beforeCreated, &beforeID, 10)
		require.NoError(t, err)
		require.Len(t, saveds, 1)
		assert.Equal(t, int64(1), saveds[0].ID)
		assert.Equal(t, int64(10), saveds[0].UserID)
		assert.Equal(t, int64(101), saveds[0].PostID)
	})

	t.Run("handles_empty_saved_posts", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		saveds, err := FuncLoadSavedPostsKeyset(ctx, 10, nil, nil, 10)
		require.NoError(t, err)
		assert.NotNil(t, saveds)
		assert.Empty(t, saveds)
	})

	t.Run("handles_scan_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{
				{"not-an-int", int64(10), int64(101), int64(1700000000)},
			}), nil
		}, nil)

		saveds, err := FuncLoadSavedPostsKeyset(ctx, 10, nil, nil, 10)
		assert.Nil(t, saveds)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		saveds, err := FuncLoadSavedPostsKeyset(ctx, 10, nil, nil, 10)
		assert.Nil(t, saveds)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{
				{int64(1), int64(10), int64(101), int64(1700000000)},
			}, errors.New("iter err")), nil
		}, nil)

		saveds, err := FuncLoadSavedPostsKeyset(ctx, 10, nil, nil, 10)
		assert.Nil(t, saveds)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
