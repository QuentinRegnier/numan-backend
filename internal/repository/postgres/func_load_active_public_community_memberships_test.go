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

func TestFuncLoadActivePublicCommunityMemberships(t *testing.T) {
	ctx := context.Background()
	cols := []string{"user_id", "conversation_id"}

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		res, err := FuncLoadActivePublicCommunityMemberships(ctx, 100, 0)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("returns_memberships_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_load_active_public_community_memberships")
			require.Len(t, args, 2)
			assert.Equal(t, int64(100), args[0].Value)
			assert.Equal(t, int64(50), args[1].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(7), int64(100)},
				{int64(8), int64(101)},
			}), nil
		}, nil)

		res, err := FuncLoadActivePublicCommunityMemberships(ctx, 100, 50)
		require.NoError(t, err)
		require.Len(t, res, 2)
		assert.Equal(t, int64(7), res[0].UserID)
		assert.Equal(t, int64(100), res[0].ConversationID)
		assert.Equal(t, int64(8), res[1].UserID)
		assert.Equal(t, int64(101), res[1].ConversationID)
	})

	t.Run("handles_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		res, err := FuncLoadActivePublicCommunityMemberships(ctx, 100, 0)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{
				{int64(7), int64(100)},
			}, errors.New("iter error")), nil
		}, nil)

		res, err := FuncLoadActivePublicCommunityMemberships(ctx, 100, 0)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
