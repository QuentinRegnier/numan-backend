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

func TestFuncGetMessageReactionsPaginated(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		reactions, err := FuncGetMessageReactionsPaginated(ctx, 10, nil, nil, 10)
		assert.Nil(t, reactions)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{"id", "message_id", "user_id", "reaction", "created_at"}
	now := time.Now().UnixMilli()

	t.Run("returns_reactions_successfully", func(t *testing.T) {
		beforeCreated := int64(1700000000)
		beforeID := int64(5)
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_get_message_reactions_keyset")
			require.Len(t, args, 4)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(1700000000), args[1].Value)
			assert.Equal(t, int64(5), args[2].Value)
			assert.Equal(t, int64(10), args[3].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(1), int64(10), int64(101), "👍", now},
				{int64(2), int64(10), int64(102), "❤️", now},
			}), nil
		}, nil)

		reactions, err := FuncGetMessageReactionsPaginated(ctx, 10, &beforeCreated, &beforeID, 10)
		require.NoError(t, err)
		require.Len(t, reactions, 2)
		assert.Equal(t, int64(1), reactions[0].ID)
		assert.Equal(t, "👍", reactions[0].Reaction)
		assert.Equal(t, "❤️", reactions[1].Reaction)
	})

	t.Run("handles_empty_reactions", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		reactions, err := FuncGetMessageReactionsPaginated(ctx, 10, nil, nil, 10)
		require.NoError(t, err)
		assert.NotNil(t, reactions)
		assert.Empty(t, reactions)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		reactions, err := FuncGetMessageReactionsPaginated(ctx, 10, nil, nil, 10)
		assert.Nil(t, reactions)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{{int64(1), int64(10), int64(101), "👍", now}}, errors.New("iter err")), nil
		}, nil)

		reactions, err := FuncGetMessageReactionsPaginated(ctx, 10, nil, nil, 10)
		assert.Nil(t, reactions)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
