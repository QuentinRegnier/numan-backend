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

func TestFuncUpdateCommunityLang(t *testing.T) {
	ctx := context.Background()
	cols := []string{"status_code", "old_lang", "new_lang", "old_pin_slot"}

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		_, err := FuncUpdateCommunityLang(ctx, 10, "en")
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("updates_lang_and_returns_cleared_pin_slot", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_update_community_lang")
			require.Len(t, args, 2)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, "en", args[1].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{"OK", "fr", "en", int64(2)},
			}), nil
		}, nil)

		res, err := FuncUpdateCommunityLang(ctx, 10, "en")
		require.NoError(t, err)
		assert.Equal(t, "OK", res.StatusCode)
		assert.Equal(t, "fr", res.OldLang)
		assert.Equal(t, "en", res.NewLang)
		assert.Equal(t, 2, res.OldPinSlot)
	})

	t.Run("updates_lang_with_null_old_pin_slot", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{
				{"OK", "fr", "es", nil},
			}), nil
		}, nil)

		res, err := FuncUpdateCommunityLang(ctx, 10, "es")
		require.NoError(t, err)
		assert.Equal(t, "OK", res.StatusCode)
		assert.Equal(t, "fr", res.OldLang)
		assert.Equal(t, "es", res.NewLang)
		assert.Equal(t, 0, res.OldPinSlot)
	})

	t.Run("handles_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		_, err := FuncUpdateCommunityLang(ctx, 10, "en")
		test.AssertDatabaseError_Postgres(t, err)
	})
}
