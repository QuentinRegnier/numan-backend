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

func TestFuncLoadCommunityMembersForTrust(t *testing.T) {
	ctx := context.Background()
	cols := []string{"conversation_id", "user_id", "role"}

	t.Run("returns_empty_without_db_call_when_conv_ids_empty", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		res, err := FuncLoadCommunityMembersForTrust(ctx, nil, 50)
		require.NoError(t, err)
		assert.Empty(t, res)
	})

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		res, err := FuncLoadCommunityMembersForTrust(ctx, []int64{10}, 50)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("returns_member_roles_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_load_community_members_for_trust")
			require.Len(t, args, 2)
			assert.Equal(t, int64(50), args[1].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(10), int64(1), 2},
				{int64(10), int64(2), 1},
				{int64(10), int64(3), 0},
			}), nil
		}, nil)

		res, err := FuncLoadCommunityMembersForTrust(ctx, []int64{10}, 50)
		require.NoError(t, err)
		require.Len(t, res, 3)
		assert.Equal(t, int64(10), res[0].ConversationID)
		assert.Equal(t, int64(1), res[0].UserID)
		assert.Equal(t, 2, res[0].Role)
		assert.Equal(t, 1, res[1].Role)
		assert.Equal(t, 0, res[2].Role)
	})

	t.Run("handles_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		res, err := FuncLoadCommunityMembersForTrust(ctx, []int64{10}, 50)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{
				{int64(10), int64(1), 2},
			}, errors.New("iter error")), nil
		}, nil)

		res, err := FuncLoadCommunityMembersForTrust(ctx, []int64{10}, 50)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
