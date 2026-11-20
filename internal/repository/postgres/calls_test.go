package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalls_ScanPosts(t *testing.T) {
	ctx := context.Background()

	t.Run("returns_empty_slice_on_nil_rows", func(t *testing.T) {
		posts, err := scanPosts(ctx, nil)
		require.NoError(t, err)
		assert.NotNil(t, posts)
		assert.Empty(t, posts)
	})

	cols := []string{
		"id", "user_id", "content", "hashtags", "indirect_hashtags", "identifiers",
		"media_ids", "visibility", "priority_level", "location", "created_at",
		"updated_at", "like_count", "comment_count", "view_count", "report_count",
		"has_media", "vector", "vector_version", "telemetry_dwell_sum", "telemetry_dwell_sq", "telemetry_clicks",
	}

	now := domain.NowMillis()

	validRow := []driver.Value{
		int64(101), int64(202), "Hello world", "{tag1,tag2}", "{itag1}", "{1,2}",
		"{1,2}", 1, 0, "Paris", now,
		now, 5, 2, 100, 0,
		true, "{0.1,0.2}", 1, float64(12.5), float64(150.25), 10,
	}

	t.Run("scans_valid_post_row_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{validRow}), nil
		}, nil)

		rows, err := infradb.PostgresDB.QueryContext(ctx, "SELECT * FROM posts")
		require.NoError(t, err)
		defer rows.Close()

		posts, err := scanPosts(ctx, rows)
		require.NoError(t, err)
		require.Len(t, posts, 1)
		assert.Equal(t, int64(101), posts[0].ID)
		assert.Equal(t, int64(202), posts[0].UserID)
		assert.Equal(t, "Hello world", posts[0].Content)
		assert.Equal(t, "Paris", posts[0].Location)
		assert.Equal(t, 5, posts[0].LikeCount)
	})

	t.Run("returns_empty_slice_when_rows_empty", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		rows, err := infradb.PostgresDB.QueryContext(ctx, "SELECT * FROM posts")
		require.NoError(t, err)
		defer rows.Close()

		posts, err := scanPosts(ctx, rows)
		require.NoError(t, err)
		assert.NotNil(t, posts)
		assert.Empty(t, posts)
	})

	t.Run("skips_corrupted_row_and_continues", func(t *testing.T) {
		corruptedRow := []driver.Value{
			"invalid-not-int", int64(202), "Corrupt", "{tag1}", "{}", "{}",
			"{}", 1, 0, nil, now,
			now, 0, 0, 0, 0,
			false, "{}", 1, float64(0), float64(0), 0,
		}

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{corruptedRow, validRow}), nil
		}, nil)

		rows, err := infradb.PostgresDB.QueryContext(ctx, "SELECT * FROM posts")
		require.NoError(t, err)
		defer rows.Close()

		posts, err := scanPosts(ctx, rows)
		require.NoError(t, err)
		require.Len(t, posts, 1)
		assert.Equal(t, int64(101), posts[0].ID)
	})

	t.Run("handles_rows_err_with_structured_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{validRow}, errors.New("stream truncated")), nil
		}, nil)

		rows, err := infradb.PostgresDB.QueryContext(ctx, "SELECT * FROM posts")
		require.NoError(t, err)
		defer rows.Close()

		posts, err := scanPosts(ctx, rows)
		assert.Nil(t, posts)
		test.AssertDatabaseError_Postgres(t, err)
	})
}

func TestCalls_ScanArticleRows(t *testing.T) {
	ctx := context.Background()

	t.Run("returns_empty_slice_on_nil_rows", func(t *testing.T) {
		articles, err := scanArticleRows(nil)
		require.NoError(t, err)
		assert.NotNil(t, articles)
		assert.Empty(t, articles)
	})

	cols := []string{
		"id", "title", "summary", "cover_media_id", "markdown_media_id",
		"attachment_media_ids", "partner_url", "visibility", "expires_at", "created_at", "updated_at",
	}

	t.Run("scans_valid_article_row_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(101), "Guide", "Résumé", int64(201), int64(301), "{401,402}", "https://partner.fr", int64(0), int64(1900000000000), int64(1700000000000), int64(1700000005000)},
			}), nil
		}, nil)

		rows, err := infradb.PostgresDB.QueryContext(ctx, "SELECT * FROM content.articles")
		require.NoError(t, err)
		defer rows.Close()

		articles, err := scanArticleRows(rows)
		require.NoError(t, err)
		require.Len(t, articles, 1)
		assert.Equal(t, int64(101), articles[0].ID)
		assert.Equal(t, []int64{401, 402}, articles[0].AttachmentMediaIDs)
		require.NotNil(t, articles[0].ExpiresAt)
		assert.Equal(t, int64(1900000000000), *articles[0].ExpiresAt)
	})

	t.Run("handles_scan_error_and_rows_err", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{
				{"bad-id", "Guide", "Résumé", int64(201), int64(301), "{}", "", int64(0), nil, int64(1), int64(1)},
			}), nil
		}, nil)

		rows, err := infradb.PostgresDB.QueryContext(ctx, "SELECT * FROM content.articles")
		require.NoError(t, err)
		defer rows.Close()

		articles, err := scanArticleRows(rows)
		assert.Nil(t, articles)
		test.AssertDatabaseError_Postgres(t, err)

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{
				{int64(101), "Guide", "Résumé", int64(201), int64(301), "{}", "", int64(0), nil, int64(1), int64(1)},
			}, errors.New("stream truncated")), nil
		}, nil)

		rows2, err := infradb.PostgresDB.QueryContext(ctx, "SELECT * FROM content.articles")
		require.NoError(t, err)
		defer rows2.Close()

		articles2, err := scanArticleRows(rows2)
		assert.Nil(t, articles2)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
