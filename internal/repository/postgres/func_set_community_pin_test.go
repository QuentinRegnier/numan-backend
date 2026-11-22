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

func TestFuncSetCommunityPin(t *testing.T) {
	ctx := context.Background()
	cols := []string{"status_code", "conv_lang", "old_pin_slot", "displaced_conv_id"}

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		_, err := FuncSetCommunityPin(ctx, 10, 1, false)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("sets_pin_with_old_slot_and_displacement", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_set_community_pin")
			require.Len(t, args, 3)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(2), args[1].Value)
			assert.Equal(t, true, args[2].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{"OK", "fr", int64(5), int64(99)},
			}), nil
		}, nil)

		res, err := FuncSetCommunityPin(ctx, 10, 2, true)
		require.NoError(t, err)
		assert.Equal(t, "OK", res.StatusCode)
		assert.Equal(t, "fr", res.ConvLang)
		assert.Equal(t, 5, res.OldPinSlot)
		assert.Equal(t, int64(99), res.DisplacedConversationID)
	})

	t.Run("sets_pin_with_null_old_slot_and_null_displacement", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{
				{"OK", "en", nil, nil},
			}), nil
		}, nil)

		res, err := FuncSetCommunityPin(ctx, 10, 1, false)
		require.NoError(t, err)
		assert.Equal(t, "OK", res.StatusCode)
		assert.Equal(t, "en", res.ConvLang)
		assert.Equal(t, 0, res.OldPinSlot)
		assert.Equal(t, int64(0), res.DisplacedConversationID)
	})

	t.Run("handles_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		_, err := FuncSetCommunityPin(ctx, 10, 1, false)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
