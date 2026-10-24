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

func TestFuncLoadCommentsKeyset(t *testing.T) {
	ctx := context.Background()

	t.Run("validates_invalid_after_created_at_format", func(t *testing.T) {
		badTime := "invalid-timestamp"
		comments, err := FuncLoadCommentsKeyset(ctx, 10, nil, &badTime, nil, 10)
		assert.Nil(t, comments)
		test.AssertBadRequestError_Postgres(t, err)
	})

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		comments, err := FuncLoadCommentsKeyset(ctx, 10, nil, nil, nil, 10)
		assert.Nil(t, comments)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{"id", "post_id", "user_id", "content", "visibility", "like_count", "score", "created_at", "updated_at"}
	now := time.Now()

	t.Run("returns_comments_successfully", func(t *testing.T) {
		validTime := now.Format(time.RFC3339)
		score := 5
		id := int64(100)

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_load_comments_keyset")
			require.Len(t, args, 5)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(5), args[1].Value)
			assert.Equal(t, validTime, args[2].Value)
			assert.Equal(t, int64(100), args[3].Value)
			assert.Equal(t, int64(10), args[4].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(1), int64(10), int64(201), "Comment 1", 1, 3, 5, validTime, validTime},
			}), nil
		}, nil)

		comments, err := FuncLoadCommentsKeyset(ctx, 10, &score, &validTime, &id, 10)
		require.NoError(t, err)
		require.Len(t, comments, 1)
		assert.Equal(t, int64(1), comments[0].ID)
		assert.Equal(t, "Comment 1", comments[0].Content)
	})

	t.Run("handles_empty_comments", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		comments, err := FuncLoadCommentsKeyset(ctx, 10, nil, nil, nil, 10)
		require.NoError(t, err)
		assert.NotNil(t, comments)
		assert.Empty(t, comments)
	})

	t.Run("handles_scan_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{
				{"not-an-int-id", int64(10), int64(201), "Comment", 1, 0, 0, now, now},
			}), nil
		}, nil)

		comments, err := FuncLoadCommentsKeyset(ctx, 10, nil, nil, nil, 10)
		assert.Nil(t, comments)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		comments, err := FuncLoadCommentsKeyset(ctx, 10, nil, nil, nil, 10)
		assert.Nil(t, comments)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{
				{int64(1), int64(10), int64(201), "Comment", 1, 0, 0, now, now},
			}, errors.New("iter err")), nil
		}, nil)

		comments, err := FuncLoadCommentsKeyset(ctx, 10, nil, nil, nil, 10)
		assert.Nil(t, comments)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
