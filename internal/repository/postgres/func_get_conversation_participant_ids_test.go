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

func TestFuncGetConversationParticipantIDs(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		ids, err := FuncGetConversationParticipantIDs(ctx, 123)
		assert.Nil(t, ids)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{"user_id"}

	t.Run("returns_participant_ids_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_get_conversation_participant_ids")
			require.Len(t, args, 1)
			assert.Equal(t, int64(123), args[0].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(10)},
				{int64(20)},
				{int64(30)},
			}), nil
		}, nil)

		ids, err := FuncGetConversationParticipantIDs(ctx, 123)
		require.NoError(t, err)
		assert.Equal(t, []int64{10, 20, 30}, ids)
	})

	t.Run("handles_empty_results", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		ids, err := FuncGetConversationParticipantIDs(ctx, 123)
		require.NoError(t, err)
		assert.NotNil(t, ids)
		assert.Empty(t, ids)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		ids, err := FuncGetConversationParticipantIDs(ctx, 123)
		assert.Nil(t, ids)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{{int64(10)}}, errors.New("stream err")), nil
		}, nil)

		ids, err := FuncGetConversationParticipantIDs(ctx, 123)
		assert.Nil(t, ids)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
