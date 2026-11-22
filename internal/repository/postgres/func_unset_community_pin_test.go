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

func TestFuncUnsetCommunityPin(t *testing.T) {
	ctx := context.Background()
	cols := []string{"status_code", "conv_lang", "old_pin_slot"}

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		_, err := FuncUnsetCommunityPin(ctx, 10)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("unsets_pin_with_previous_slot", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_unset_community_pin")
			require.Len(t, args, 1)
			assert.Equal(t, int64(10), args[0].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{"OK", "fr", int64(3)},
			}), nil
		}, nil)

		res, err := FuncUnsetCommunityPin(ctx, 10)
		require.NoError(t, err)
		assert.Equal(t, "OK", res.StatusCode)
		assert.Equal(t, "fr", res.ConvLang)
		assert.Equal(t, 3, res.OldPinSlot)
	})

	t.Run("unsets_pin_idempotently_when_unpinned", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{
				{"OK", "fr", nil},
			}), nil
		}, nil)

		res, err := FuncUnsetCommunityPin(ctx, 10)
		require.NoError(t, err)
		assert.Equal(t, "OK", res.StatusCode)
		assert.Equal(t, 0, res.OldPinSlot)
	})

	t.Run("handles_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		_, err := FuncUnsetCommunityPin(ctx, 10)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
