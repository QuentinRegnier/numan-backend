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

func TestFuncFilterUnreferencedArticleMedia(t *testing.T) {
	ctx := context.Background()

	t.Run("empty_ids_returns_empty_slice", func(t *testing.T) {
		emptyUnref, err := repoPostgres.FuncFilterUnreferencedArticleMedia(ctx, nil, 0)
		require.NoError(t, err)
		assert.Empty(t, emptyUnref)
	})

	t.Run("uninitialized_database", func(t *testing.T) {
		origDB := infraPostgres.PostgresDB
		infraPostgres.PostgresDB = nil
		defer func() { infraPostgres.PostgresDB = origDB }()

		res, err := repoPostgres.FuncFilterUnreferencedArticleMedia(ctx, []int64{10}, 0)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("success", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_filter_unreferenced_article_media")
			require.Len(t, args, 2)
			return test.NewMockRows([]string{"media_id"}, [][]driver.Value{
				{int64(401)},
			}), nil
		}, nil)

		unref, err := repoPostgres.FuncFilterUnreferencedArticleMedia(ctx, []int64{401, 402}, 101)
		require.NoError(t, err)
		assert.Equal(t, []int64{401}, unref)
	})

	t.Run("query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db query err")
		}, nil)

		res, err := repoPostgres.FuncFilterUnreferencedArticleMedia(ctx, []int64{401}, 0)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
