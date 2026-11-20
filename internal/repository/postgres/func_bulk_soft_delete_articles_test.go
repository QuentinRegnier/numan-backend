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

func TestFuncBulkSoftDeleteArticles(t *testing.T) {
	ctx := context.Background()

	t.Run("empty_ids_noop", func(t *testing.T) {
		require.NoError(t, repoPostgres.FuncBulkSoftDeleteArticles(ctx, nil))
	})

	t.Run("uninitialized_database", func(t *testing.T) {
		origDB := infraPostgres.PostgresDB
		infraPostgres.PostgresDB = nil
		defer func() { infraPostgres.PostgresDB = origDB }()

		err := repoPostgres.FuncBulkSoftDeleteArticles(ctx, []int64{1})
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("success", func(t *testing.T) {
		test.SetupMockDB(t, nil, func(query string, args []driver.NamedValue) (driver.Result, error) {
			assert.Contains(t, query, "content.func_bulk_soft_delete_articles")
			require.Len(t, args, 1)
			return driver.RowsAffected(1), nil
		})

		require.NoError(t, repoPostgres.FuncBulkSoftDeleteArticles(ctx, []int64{101}))
	})

	t.Run("exec_error", func(t *testing.T) {
		test.SetupMockDB(t, nil, func(query string, args []driver.NamedValue) (driver.Result, error) {
			return nil, errors.New("db exec err")
		})

		err := repoPostgres.FuncBulkSoftDeleteArticles(ctx, []int64{101})
		test.AssertDatabaseError_Postgres(t, err)
	})
}
