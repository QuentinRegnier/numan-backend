package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncLoadLikes(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		likes, err := FuncLoadLikes(ctx, 1, 10, 20, 10, 0)
		assert.Nil(t, likes)
		assertDatabaseError(t, err)
	})

	cols := []string{"id", "target_type", "target_id", "user_id", "created_at"}
	now := time.Now().UTC()

	t.Run("returns_likes_successfully", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_load_likes")
			require.Len(t, args, 5)
			assert.Equal(t, int64(1), args[0].Value)
			assert.Equal(t, int64(10), args[1].Value)
			assert.Equal(t, int64(20), args[2].Value)
			assert.Equal(t, int64(10), args[3].Value)
			assert.Equal(t, int64(0), args[4].Value)
			return newMockRows(cols, [][]driver.Value{
				{int64(1), 1, int64(10), int64(20), now},
			}), nil
		}, nil)

		likes, err := FuncLoadLikes(ctx, 1, 10, 20, 10, 0)
		require.NoError(t, err)
		require.Len(t, likes, 1)
		assert.Equal(t, int64(1), likes[0].ID)
		assert.Equal(t, 1, likes[0].TargetType)
		assert.Equal(t, int64(10), likes[0].TargetID)
		assert.Equal(t, int64(20), likes[0].UserID)
		assert.Equal(t, now.Format(time.RFC3339), likes[0].CreatedAt)
	})

	t.Run("handles_null_filter_parameters", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			require.Len(t, args, 5)
			assert.Nil(t, args[0].Value)
			assert.Nil(t, args[1].Value)
			assert.Nil(t, args[2].Value)
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		likes, err := FuncLoadLikes(ctx, -1, 0, 0, 10, 0)
		require.NoError(t, err)
		assert.NotNil(t, likes)
		assert.Empty(t, likes)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		likes, err := FuncLoadLikes(ctx, 1, 10, 20, 10, 0)
		assert.Nil(t, likes)
		assertDatabaseError(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRowsWithErr(cols, [][]driver.Value{
				{int64(1), 1, int64(10), int64(20), now},
			}, errors.New("iter err")), nil
		}, nil)

		likes, err := FuncLoadLikes(ctx, 1, 10, 20, 10, 0)
		assert.Nil(t, likes)
		assertDatabaseError(t, err)
	})
}
