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

func TestFuncLoadActiveCommunityPins(t *testing.T) {
	ctx := context.Background()
	cols := []string{"conversation_id", "lang", "pin_slot"}

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		res, err := FuncLoadActiveCommunityPins(ctx)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("returns_active_pins_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_load_active_community_pins")
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(10), "fr", 1},
				{int64(20), "en", 3},
			}), nil
		}, nil)

		res, err := FuncLoadActiveCommunityPins(ctx)
		require.NoError(t, err)
		require.Len(t, res, 2)
		assert.Equal(t, int64(10), res[0].ConversationID)
		assert.Equal(t, "fr", res[0].Lang)
		assert.Equal(t, 1, res[0].PinSlot)
		assert.Equal(t, int64(20), res[1].ConversationID)
		assert.Equal(t, "en", res[1].Lang)
		assert.Equal(t, 3, res[1].PinSlot)
	})

	t.Run("handles_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		res, err := FuncLoadActiveCommunityPins(ctx)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{
				{int64(10), "fr", 1},
			}, errors.New("iter error")), nil
		}, nil)

		res, err := FuncLoadActiveCommunityPins(ctx)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
