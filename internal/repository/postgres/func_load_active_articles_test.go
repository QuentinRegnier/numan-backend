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

func TestFuncLoadActiveArticles(t *testing.T) {
	ctx := context.Background()

	t.Run("uninitialized_database", func(t *testing.T) {
		origDB := infraPostgres.PostgresDB
		infraPostgres.PostgresDB = nil
		defer func() { infraPostgres.PostgresDB = origDB }()

		res, err := repoPostgres.FuncLoadActiveArticles(ctx)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{
		"id", "title", "summary", "cover_media_id", "markdown_media_id",
		"attachment_media_ids", "partner_url", "visibility", "expires_at", "created_at", "updated_at",
	}

	t.Run("success", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_load_active_articles")
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(101), "Guide", "Résumé", int64(201), int64(301), "{}", "", int64(0), nil, int64(1700000000000), int64(1700000000000)},
			}), nil
		}, nil)

		active, err := repoPostgres.FuncLoadActiveArticles(ctx)
		require.NoError(t, err)
		require.Len(t, active, 1)
		assert.Equal(t, int64(101), active[0].ID)
		assert.Empty(t, active[0].AttachmentMediaIDs)
	})

	t.Run("query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db query err")
		}, nil)

		res, err := repoPostgres.FuncLoadActiveArticles(ctx)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
