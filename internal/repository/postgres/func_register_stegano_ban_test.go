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

func TestFuncRegisterSteganoBan(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		err := FuncRegisterSteganoBan(ctx, 1, "A:1", "reason", 10)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("registers_stegano_ban_successfully", func(t *testing.T) {
		test.SetupMockDB(t, nil, func(query string, args []driver.NamedValue) (driver.Result, error) {
			assert.Contains(t, query, "moderation.func_register_stegano_ban")
			require.Len(t, args, 4)
			assert.Equal(t, int64(77), args[0].Value)
			assert.Equal(t, "A:77", args[1].Value)
			assert.Equal(t, "Non-consenti", args[2].Value)
			assert.Equal(t, int64(500), args[3].Value)
			return driver.RowsAffected(1), nil
		})

		err := FuncRegisterSteganoBan(ctx, 77, "A:77", "Non-consenti", 500)
		require.NoError(t, err)
	})

	t.Run("registers_stegano_ban_without_source_post", func(t *testing.T) {
		test.SetupMockDB(t, nil, func(query string, args []driver.NamedValue) (driver.Result, error) {
			assert.Contains(t, query, "moderation.func_register_stegano_ban")
			require.Len(t, args, 4)
			assert.Equal(t, int64(88), args[0].Value)
			assert.Nil(t, args[3].Value)
			return driver.RowsAffected(1), nil
		})

		err := FuncRegisterSteganoBan(ctx, 88, "A:88", "Fuite de média", 0)
		require.NoError(t, err)
	})

	t.Run("handles_database_exec_error", func(t *testing.T) {
		test.SetupMockDB(t, nil, func(query string, args []driver.NamedValue) (driver.Result, error) {
			return nil, errors.New("exec failed")
		})

		err := FuncRegisterSteganoBan(ctx, 77, "A:77", "Raison", 500)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
