package postgres_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	infraPostgres "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoPostgres "github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncGetArticleByID(t *testing.T) {
	ctx := context.Background()

	t.Run("non_positive_id_returns_not_found", func(t *testing.T) {
		_, found, err := repoPostgres.FuncGetArticleByID(ctx, 0)
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("uninitialized_database", func(t *testing.T) {
		origDB := infraPostgres.PostgresDB
		infraPostgres.PostgresDB = nil
		defer func() { infraPostgres.PostgresDB = origDB }()

		_, found, err := repoPostgres.FuncGetArticleByID(ctx, 1)
		assert.False(t, found)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{
		"id", "title", "summary", "cover_media_id", "markdown_media_id",
		"attachment_media_ids", "partner_url", "visibility", "expires_at", "created_at", "updated_at",
	}

	t.Run("found_and_not_found", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_get_article_by_id")
			if len(args) > 0 && args[0].Value.(int64) == 999 {
				return test.NewMockRows(cols, nil), nil
			}
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(101), "Guide", "Résumé", int64(201), int64(301), "{401,402}", "https://partner.fr", int64(0), int64(1900000000000), int64(1700000000000), int64(1700000005000)},
			}), nil
		}, nil)

		art, found, err := repoPostgres.FuncGetArticleByID(ctx, 101)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, int64(101), art.ID)
		assert.Equal(t, []int64{401, 402}, art.AttachmentMediaIDs)
		require.NotNil(t, art.ExpiresAt)
		assert.Equal(t, int64(1900000000000), *art.ExpiresAt)

		_, foundMissing, errMissing := repoPostgres.FuncGetArticleByID(ctx, 999)
		require.NoError(t, errMissing)
		assert.False(t, foundMissing)
	})

	t.Run("query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db query err")
		}, nil)

		_, found, err := repoPostgres.FuncGetArticleByID(ctx, 101)
		assert.False(t, found)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
