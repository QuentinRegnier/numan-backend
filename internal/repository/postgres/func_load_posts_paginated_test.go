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

func TestFuncLoadPostsPaginated(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		posts, err := FuncLoadPostsPaginated(ctx, 10, 0)
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

	t.Run("returns_paginated_posts_successfully", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_load_posts_paginated")
			require.Len(t, args, 2)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(0), args[1].Value)
			return newMockRows(cols, [][]driver.Value{
				{
					int64(101), int64(202), "Paginated post", "{tag}", "{}", "{}",
					"{}", 1, 0, "Paris", now.UnixMilli(),
					now.UnixMilli(), 10, 1, 50, 0,
					false, "{0.5}", 1, float64(10), float64(100), 5,
				},
			}), nil
		}, nil)

		posts, err := FuncLoadPostsPaginated(ctx, 10, 0)
		require.NoError(t, err)
		require.Len(t, posts, 1)
		assert.Equal(t, int64(101), posts[0].ID)
		assert.Equal(t, "Paginated post", posts[0].Content)
	})

	t.Run("handles_empty_posts", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		posts, err := FuncLoadPostsPaginated(ctx, 10, 0)
		require.NoError(t, err)
		assert.NotNil(t, posts)
		assert.Empty(t, posts)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query failed")
		}, nil)

		posts, err := FuncLoadPostsPaginated(ctx, 10, 0)
		assert.Nil(t, posts)
		assertDatabaseError(t, err)
	})
}
