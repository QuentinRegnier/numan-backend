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

func TestFuncIsSteganoBanned(t *testing.T) {
	ctx := context.Background()
	cols := []string{"banned"}

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		banned, err := FuncIsSteganoBanned(ctx, 1)
		assert.False(t, banned)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("returns_true_when_user_is_stegano_banned", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "moderation.func_is_stegano_banned")
			require.Len(t, args, 1)
			assert.Equal(t, int64(77), args[0].Value)
			return test.NewMockRows(cols, [][]driver.Value{{true}}), nil
		}, nil)

		banned, err := FuncIsSteganoBanned(ctx, 77)
		require.NoError(t, err)
		assert.True(t, banned)
	})

	t.Run("returns_false_when_user_is_not_stegano_banned", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "moderation.func_is_stegano_banned")
			require.Len(t, args, 1)
			assert.Equal(t, int64(12), args[0].Value)
			return test.NewMockRows(cols, [][]driver.Value{{false}}), nil
		}, nil)

		banned, err := FuncIsSteganoBanned(ctx, 12)
		require.NoError(t, err)
		assert.False(t, banned)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db query error")
		}, nil)

		banned, err := FuncIsSteganoBanned(ctx, 77)
		assert.False(t, banned)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
