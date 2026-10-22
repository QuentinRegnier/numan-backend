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

func TestFuncLoadUserPostsKeyset(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		posts, err := FuncLoadUserPostsKeyset(ctx, 10, nil, nil, 10)
		assert.Nil(t, posts)
		assertDatabaseError(t, err)
	})

	cols := []string{
		"id", "user_id", "content", "hashtags", "indirect_hashtags", "identifiers",
		"media_ids", "visibility", "priority_level", "location", "created_at",
		"updated_at", "like_count", "comment_count", "view_count", "report_count",
		"has_media", "vector", "vector_version", "telemetry_dwell_sum", "telemetry_dwell_sq", "telemetry_clicks",
	}
	now := time.Now()

	t.Run("returns_user_posts_successfully", func(t *testing.T) {
		beforeCreated := int64(1700000000)
		beforeID := int64(50)

		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_load_user_posts_keyset")
			require.Len(t, args, 4)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(1700000000), args[1].Value)
			assert.Equal(t, int64(50), args[2].Value)
			assert.Equal(t, int64(10), args[3].Value)
			return newMockRows(cols, [][]driver.Value{
				{
					int64(101), int64(10), "User post", "{art}", "{}", "{}",
					"{}", 1, 0, nil, now.UnixMilli(),
					now.UnixMilli(), 4, 1, 20, 0,
					false, "{}", 1, float64(0), float64(0), 0,
				},
			}), nil
		}, nil)

		posts, err := FuncLoadUserPostsKeyset(ctx, 10, &beforeCreated, &beforeID, 10)
		require.NoError(t, err)
		require.Len(t, posts, 1)
		assert.Equal(t, int64(101), posts[0].ID)
		assert.Equal(t, int64(10), posts[0].UserID)
		assert.Equal(t, "User post", posts[0].Content)
	})

	t.Run("handles_empty_user_posts", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		posts, err := FuncLoadUserPostsKeyset(ctx, 10, nil, nil, 10)
		require.NoError(t, err)
		assert.NotNil(t, posts)
		assert.Empty(t, posts)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		posts, err := FuncLoadUserPostsKeyset(ctx, 10, nil, nil, 10)
		assert.Nil(t, posts)
		assertDatabaseError(t, err)
	})
}
