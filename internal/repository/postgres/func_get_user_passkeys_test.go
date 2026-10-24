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

func TestFuncGetUserPasskeys(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		passkeys, err := FuncGetUserPasskeys(ctx, 10)
		assert.Nil(t, passkeys)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{
		"id", "user_id", "credential_id", "public_key", "attestation_type",
		"aaguid", "sign_count", "device_name", "transports", "created_at", "updated_at",
	}
	now := time.Now().UnixMilli()

	t.Run("returns_user_passkeys_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_get_user_passkeys")
			require.Len(t, args, 1)
			assert.Equal(t, int64(10), args[0].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(1), int64(10), "cred1", []byte("pub1"), "none", []byte("aaguid1"), int64(3), "iPhone", "{internal}", now, now},
				{int64(2), int64(10), "cred2", []byte("pub2"), "none", []byte("aaguid2"), int64(8), "YubiKey", "{usb,nfc}", now, now},
			}), nil
		}, nil)

		passkeys, err := FuncGetUserPasskeys(ctx, 10)
		require.NoError(t, err)
		require.Len(t, passkeys, 2)
		assert.Equal(t, "cred1", passkeys[0].CredentialID)
		assert.Equal(t, "iPhone", passkeys[0].DeviceName)
		assert.Equal(t, []string{"internal"}, passkeys[0].Transports)
		assert.Equal(t, "cred2", passkeys[1].CredentialID)
	})

	t.Run("handles_empty_passkeys", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		passkeys, err := FuncGetUserPasskeys(ctx, 10)
		require.NoError(t, err)
		assert.NotNil(t, passkeys)
		assert.Empty(t, passkeys)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		passkeys, err := FuncGetUserPasskeys(ctx, 10)
		assert.Nil(t, passkeys)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{
				{int64(1), int64(10), "cred1", []byte("pub1"), "none", []byte("aaguid1"), int64(3), "iPhone", "{internal}", now, now},
			}, errors.New("iter err")), nil
		}, nil)

		passkeys, err := FuncGetUserPasskeys(ctx, 10)
		assert.Nil(t, passkeys)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
