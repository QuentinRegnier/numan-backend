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

func TestFuncLoadPostsForTimeDecay(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		posts, err := FuncLoadPostsForTimeDecay(ctx, "1 day", "7 days")
		assert.Nil(t, posts)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{
		"id", "like_count", "comment_count", "view_count", "has_media",
		"created_at", "hashtags", "indirect_hashtags", "visibility", "priority_level", "report_count",
	}
	now := time.Now()

	t.Run("returns_time_decay_posts_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_load_posts_for_time_decay")
			require.Len(t, args, 2)
			assert.Equal(t, "1 day", args[0].Value)
			assert.Equal(t, "7 days", args[1].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(100), 5, 2, 50, true, now, "{tag1,tag2}", "{itag1}", 1, 0, 0},
			}), nil
		}, nil)

		posts, err := FuncLoadPostsForTimeDecay(ctx, "1 day", "7 days")
		require.NoError(t, err)
		require.Len(t, posts, 1)
		assert.Equal(t, int64(100), posts[0].ID)
		assert.Equal(t, 5, posts[0].LikeCount)
		assert.Equal(t, 2, posts[0].CommentCount)
		assert.Equal(t, 50, posts[0].ViewCount)
		assert.True(t, posts[0].HasMedia)
		assert.Equal(t, []string{"tag1", "tag2"}, posts[0].Hashtags)
		assert.Equal(t, []string{"itag1"}, posts[0].IndirectHashtags)
	})

	t.Run("handles_empty_posts", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		posts, err := FuncLoadPostsForTimeDecay(ctx, "1 day", "7 days")
		require.NoError(t, err)
		assert.NotNil(t, posts)
		assert.Empty(t, posts)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db failure")
		}, nil)

		posts, err := FuncLoadPostsForTimeDecay(ctx, "1 day", "7 days")
		assert.Nil(t, posts)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{
				{int64(100), 0, 0, 0, false, now, "{}", "{}", 1, 0, 0},
			}, errors.New("iter err")), nil
		}, nil)

		posts, err := FuncLoadPostsForTimeDecay(ctx, "1 day", "7 days")
		assert.Nil(t, posts)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
