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

func TestFuncLoadSession(t *testing.T) {
	ctx := context.Background()

	t.Run("validates_empty_parameters_returning_bad_request", func(t *testing.T) {
		s, err := FuncLoadSession(ctx, -1, -1, "", "")
		assert.Empty(t, s.ID)
		test.AssertBadRequestError_Postgres(t, err)
	})

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		s, err := FuncLoadSession(ctx, 100, -1, "", "")
		assert.Empty(t, s.ID)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{
		"id", "user_id", "master_token", "firebase_installation_id",
		"device_info", "ip_history", "created_at", "expires_at",
	}
	now := time.Now().UnixMilli()
	future := now + 3600000

	t.Run("returns_session_successfully", func(t *testing.T) {
		devJSON := `{"platform":"iOS","os_version":"17.0"}`
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_load_sessions")
			require.Len(t, args, 4)
			assert.Equal(t, int64(100), args[0].Value)
			assert.Nil(t, args[1].Value)
			assert.Nil(t, args[2].Value)
			assert.Nil(t, args[3].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(100), int64(1), "tok_master", "firebase_tok", []byte(devJSON), "{127.0.0.1}", now, future},
			}), nil
		}, nil)

		s, err := FuncLoadSession(ctx, 100, -1, "", "")
		require.NoError(t, err)
		assert.Equal(t, int64(100), s.ID)
		assert.Equal(t, int64(1), s.UserID)
		assert.Equal(t, "tok_master", s.MasterToken)
		assert.Equal(t, "firebase_tok", s.FirebaseInstallationID)
		assert.Equal(t, []string{"127.0.0.1"}, []string(s.IPHistory))
		assert.Equal(t, "iOS", s.DeviceInfo["platform"])
	})

	t.Run("handles_no_rows_returning_empty_payload_and_nil_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		s, err := FuncLoadSession(ctx, 999, -1, "", "")
		require.NoError(t, err)
		assert.Empty(t, s.ID)
	})

	t.Run("handles_invalid_device_info_json", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(100), int64(1), "tok", nil, []byte("invalid-json"), "{}", now, now},
			}), nil
		}, nil)

		s, err := FuncLoadSession(ctx, 100, -1, "", "")
		assert.Empty(t, s.ID)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		s, err := FuncLoadSession(ctx, 100, -1, "", "")
		assert.Empty(t, s.ID)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
