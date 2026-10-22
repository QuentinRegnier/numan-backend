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

func TestFuncLoadUserSessionsView(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		sessions, err := FuncLoadUserSessionsView(ctx, 10)
		assert.Nil(t, sessions)
		assertDatabaseError(t, err)
	})

	cols := []string{"id", "device_info", "ip_history", "created_at", "expires_at"}
	now := time.Now().UnixMilli()
	future := now + 3600000

	t.Run("returns_session_views_successfully", func(t *testing.T) {
		devJSON := `{"browser":"Chrome"}`
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_get_active_sessions_view")
			require.Len(t, args, 1)
			assert.Equal(t, int64(10), args[0].Value)
			return newMockRows(cols, [][]driver.Value{
				{int64(1), []byte(devJSON), "{192.168.1.1}", now, future},
			}), nil
		}, nil)

		sessions, err := FuncLoadUserSessionsView(ctx, 10)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		assert.Equal(t, int64(1), sessions[0].ID)
		assert.Equal(t, "Chrome", sessions[0].DeviceInfo["browser"])
		assert.Equal(t, []string{"192.168.1.1"}, sessions[0].IPHistory)
	})

	t.Run("handles_invalid_device_info_json", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{
				{int64(1), []byte("corrupt-json"), "{}", now, now},
			}), nil
		}, nil)

		sessions, err := FuncLoadUserSessionsView(ctx, 10)
		assert.Nil(t, sessions)
		assertDatabaseError(t, err)
	})

	t.Run("handles_empty_sessions", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		sessions, err := FuncLoadUserSessionsView(ctx, 10)
		require.NoError(t, err)
		assert.NotNil(t, sessions)
		assert.Empty(t, sessions)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query failed")
		}, nil)

		sessions, err := FuncLoadUserSessionsView(ctx, 10)
		assert.Nil(t, sessions)
		assertDatabaseError(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRowsWithErr(cols, [][]driver.Value{
				{int64(1), nil, "{}", now, now},
			}, errors.New("iter err")), nil
		}, nil)

		sessions, err := FuncLoadUserSessionsView(ctx, 10)
		assert.Nil(t, sessions)
		assertDatabaseError(t, err)
	})
}
