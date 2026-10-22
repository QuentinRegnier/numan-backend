package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncGetPasskeyByCredentialID(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		p, err := FuncGetPasskeyByCredentialID(ctx, "cred123")
		assert.Empty(t, p.ID)
		assertDatabaseError(t, err)
	})

	cols := []string{
		"id", "user_id", "credential_id", "public_key", "attestation_type",
		"aaguid", "sign_count", "device_name", "transports", "created_at", "updated_at",
	}
	now := time.Now().UnixMilli()

	t.Run("returns_passkey_successfully", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_get_passkey_by_credential_id")
			require.Len(t, args, 1)
			assert.Equal(t, "cred123", args[0].Value)
			return newMockRows(cols, [][]driver.Value{
				{int64(1), int64(10), "cred123", []byte("pubkey"), "none", []byte("aaguid"), int64(5), "MacBook", "{usb,ble}", now, now},
			}), nil
		}, nil)

		p, err := FuncGetPasskeyByCredentialID(ctx, "cred123")
		require.NoError(t, err)
		assert.Equal(t, int64(1), p.ID)
		assert.Equal(t, int64(10), p.UserID)
		assert.Equal(t, "cred123", p.CredentialID)
		assert.Equal(t, []byte("pubkey"), p.PublicKey)
		assert.Equal(t, int64(5), p.SignCount)
		assert.Equal(t, "MacBook", p.DeviceName)
		assert.Equal(t, []string{"usb", "ble"}, p.Transports)
	})

	t.Run("handles_no_rows_returning_empty_payload_and_nil_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		p, err := FuncGetPasskeyByCredentialID(ctx, "unknown")
		require.NoError(t, err)
		assert.Empty(t, p.ID)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query error")
		}, nil)

		p, err := FuncGetPasskeyByCredentialID(ctx, "cred123")
		assert.Empty(t, p.ID)
		assertDatabaseError(t, err)
	})
}
