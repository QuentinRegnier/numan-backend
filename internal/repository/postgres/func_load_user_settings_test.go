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

func TestFuncLoadUserSettings(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		s, err := FuncLoadUserSettings(ctx, 1)
		assert.Empty(t, s.ID)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{
		"id", "user_id", "privacy", "notifications", "display_and_content",
		"telemetry_vector", "telemetry_tags", "telemetry_timestamp",
	}
	now := time.Now()

	t.Run("returns_user_settings_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_load_user_settings")
			require.Len(t, args, 1)
			assert.Equal(t, int64(1), args[0].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{
					int64(10), int64(1), []byte(`{"profile_visibility":0,"show_online_status":true}`),
					[]byte(`{"master_push_enabled":true}`), []byte(`{"theme":2}`),
					"{0.1,0.2}", "{tech,news}", now.UnixMilli(),
				},
			}), nil
		}, nil)

		s, err := FuncLoadUserSettings(ctx, 1)
		require.NoError(t, err)
		assert.Equal(t, int64(10), s.ID)
		assert.Equal(t, int64(1), s.UserID)
		assert.Equal(t, 0, s.Privacy.ProfileVisibility)
		assert.True(t, s.Privacy.ShowOnlineStatus)
		assert.True(t, s.Notifications.MasterPushEnabled)
		assert.Equal(t, 2, s.DisplayAndContent.Theme)
		assert.Equal(t, []float32{0.1, 0.2}, s.TelemetryVector)
		assert.Equal(t, []string{"tech", "news"}, s.TelemetryTags)
	})

	t.Run("handles_no_rows_as_default_settings", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		s, err := FuncLoadUserSettings(ctx, 999)
		require.NoError(t, err)
		assert.Empty(t, s.ID)
	})

	t.Run("handles_invalid_privacy_json", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(10), int64(1), []byte("corrupt-json"), nil, nil, "{}", "{}", now.UnixMilli()},
			}), nil
		}, nil)

		s, err := FuncLoadUserSettings(ctx, 1)
		assert.Empty(t, s.ID)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query failed")
		}, nil)

		s, err := FuncLoadUserSettings(ctx, 1)
		assert.Empty(t, s.ID)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
