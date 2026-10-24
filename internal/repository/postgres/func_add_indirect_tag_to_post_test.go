package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncAddIndirectTagToPost(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		err := FuncAddIndirectTagToPost(ctx, 100, "golang")
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("adds_indirect_tag_successfully", func(t *testing.T) {
		test.SetupMockDB(t, nil, func(query string, args []driver.NamedValue) (driver.Result, error) {
			assert.Contains(t, query, "content.func_add_indirect_tag_to_post")
			require.Len(t, args, 2)
			assert.Equal(t, int64(100), args[0].Value)
			assert.Equal(t, "golang", args[1].Value)
			return driver.RowsAffected(1), nil
		})

		err := FuncAddIndirectTagToPost(ctx, 100, "golang")
		require.NoError(t, err)
	})

	t.Run("handles_exec_error", func(t *testing.T) {
		test.SetupMockDB(t, nil, func(query string, args []driver.NamedValue) (driver.Result, error) {
			return nil, errors.New("exec failed")
		})

		err := FuncAddIndirectTagToPost(ctx, 100, "golang")
		test.AssertDatabaseError_Postgres(t, err)
	})
}
