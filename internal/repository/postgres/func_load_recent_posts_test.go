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

func TestFuncLoadRecentPosts(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		posts, err := FuncLoadRecentPosts(ctx, 7)
		assert.Nil(t, posts)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{
		"id", "user_id", "content", "hashtags", "indirect_hashtags", "identifiers",
		"media_ids", "visibility", "priority_level", "location", "created_at",
		"updated_at", "like_count", "comment_count", "view_count", "report_count",
		"has_media", "vector", "vector_version", "telemetry_dwell_sum", "telemetry_dwell_sq", "telemetry_clicks",
	}
	now := time.Now()

	t.Run("returns_recent_posts_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_load_recent_posts")
			require.Len(t, args, 1)
			assert.Equal(t, int64(7), args[0].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{
					int64(101), int64(202), "Recent post", "{}", "{}", "{}",
					"{}", 1, 0, nil, now.UnixMilli(),
					now.UnixMilli(), 0, 0, 0, 0,
					false, "{}", 1, float64(0), float64(0), 0,
				},
			}), nil
		}, nil)

		posts, err := FuncLoadRecentPosts(ctx, 7)
		require.NoError(t, err)
		require.Len(t, posts, 1)
		assert.Equal(t, int64(101), posts[0].ID)
		assert.Equal(t, "Recent post", posts[0].Content)
	})

	t.Run("handles_empty_recent_posts", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		posts, err := FuncLoadRecentPosts(ctx, 7)
		require.NoError(t, err)
		assert.NotNil(t, posts)
		assert.Empty(t, posts)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query failed")
		}, nil)

		posts, err := FuncLoadRecentPosts(ctx, 7)
		assert.Nil(t, posts)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
