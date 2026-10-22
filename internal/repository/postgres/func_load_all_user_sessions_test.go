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

func TestFuncLoadAllUserSessions(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		sessions, err := FuncLoadAllUserSessions(ctx, 1)
		assert.Nil(t, sessions)
		assertDatabaseError(t, err)
	})

	cols := []string{
		"id", "user_id", "master_token", "firebase_installation_id",
		"device_info", "ip_history", "created_at", "expires_at",
	}
	now := time.Now().UnixMilli()
	future := now + 3600000

	t.Run("returns_all_user_sessions_successfully", func(t *testing.T) {
		devJSON := `{"model":"iPhone 15"}`
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_load_sessions")
			require.Len(t, args, 1)
			assert.Equal(t, int64(1), args[0].Value)
			return newMockRows(cols, [][]driver.Value{
				{int64(10), int64(1), "tok1", "fb1", []byte(devJSON), "{192.168.1.1}", now, future},
				{int64(20), int64(1), "tok2", "fb2", nil, "{10.0.0.1}", now, future},
			}), nil
		}, nil)

		sessions, err := FuncLoadAllUserSessions(ctx, 1)
		require.NoError(t, err)
		require.Len(t, sessions, 2)
		assert.Equal(t, int64(10), sessions[0].ID)
		assert.Equal(t, "iPhone 15", sessions[0].DeviceInfo["model"])
		assert.Equal(t, []string{"192.168.1.1"}, []string(sessions[0].IPHistory))
		assert.Equal(t, int64(20), sessions[1].ID)
	})

	t.Run("handles_invalid_device_info_json", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{
				{int64(10), int64(1), "tok", "fb", []byte("corrupt-json"), "{}", now, future},
			}), nil
		}, nil)

		sessions, err := FuncLoadAllUserSessions(ctx, 1)
		assert.Nil(t, sessions)
		assertDatabaseError(t, err)
	})

	t.Run("handles_empty_sessions", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		sessions, err := FuncLoadAllUserSessions(ctx, 999)
		require.NoError(t, err)
		assert.NotNil(t, sessions)
		assert.Empty(t, sessions)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		sessions, err := FuncLoadAllUserSessions(ctx, 1)
		assert.Nil(t, sessions)
		assertDatabaseError(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRowsWithErr(cols, [][]driver.Value{
				{int64(10), int64(1), "tok", nil, nil, "{}", now, now},
			}, errors.New("iter err")), nil
		}, nil)

		sessions, err := FuncLoadAllUserSessions(ctx, 1)
		assert.Nil(t, sessions)
		assertDatabaseError(t, err)
	})
}
