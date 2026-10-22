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

func TestFuncGetComment(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		c, err := FuncGetComment(ctx, 100)
		assert.Empty(t, c.ID)
		assertDatabaseError(t, err)
	})

	cols := []string{"id", "post_id", "user_id", "content", "visibility", "like_count", "score", "created_at", "updated_at"}
	now := time.Now()

	t.Run("returns_the_comment_successfully", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_get_comment")
			require.Len(t, args, 1)
			assert.Equal(t, int64(100), args[0].Value)
			return newMockRows(cols, [][]driver.Value{
				{int64(100), int64(200), int64(300), "Great post!", 1, 10, 42, now, now},
			}), nil
		}, nil)

		c, err := FuncGetComment(ctx, 100)
		require.NoError(t, err)
		assert.Equal(t, int64(100), c.ID)
		assert.Equal(t, int64(200), c.PostID)
		assert.Equal(t, int64(300), c.UserID)
		assert.Equal(t, "Great post!", c.Content)
		assert.Equal(t, 10, c.LikeCount)
		assert.Equal(t, 42, c.Score)
	})

	t.Run("handles_comment_not_found", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		c, err := FuncGetComment(ctx, 999)
		assert.Empty(t, c.ID)
		assertNotFoundError(t, err, "COMMENT_NOT_FOUND")
	})

	t.Run("returns_structured_error_on_database_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		c, err := FuncGetComment(ctx, 100)
		assert.Empty(t, c.ID)
		assertDatabaseError(t, err)
	})
}
