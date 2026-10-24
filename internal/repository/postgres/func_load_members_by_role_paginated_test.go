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

func TestFuncLoadMembersByRolePaginated(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		members, err := FuncLoadMembersByRolePaginated(ctx, 10, -3, 10, 0)
		assert.Nil(t, members)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{
		"id", "conversation_id", "user_id", "role",
		"joined_at", "unread_count", "frozen_message_id", "last_read_message_id", "created_at", "updated_at",
	}
	now := time.Now()

	t.Run("returns_members_by_role_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_load_members_by_role_paginated")
			require.Len(t, args, 4)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(-3), args[1].Value)
			assert.Equal(t, int64(10), args[2].Value)
			assert.Equal(t, int64(0), args[3].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(1), int64(10), int64(101), -3, now.UnixMilli(), 0, int64(50), int64(60), now.UnixMilli(), now.UnixMilli()},
				{int64(2), int64(10), int64(102), -3, now.UnixMilli(), 0, nil, nil, now.UnixMilli(), now.UnixMilli()},
			}), nil
		}, nil)

		members, err := FuncLoadMembersByRolePaginated(ctx, 10, -3, 10, 0)
		require.NoError(t, err)
		require.Len(t, members, 2)
		assert.Equal(t, int64(1), members[0].ID)
		assert.Equal(t, int64(101), members[0].UserID)
		assert.Equal(t, int64(50), members[0].FrozenMessageID)
		assert.Equal(t, int64(60), members[0].LastReadMessageID)

		assert.Equal(t, int64(2), members[1].ID)
		assert.Equal(t, int64(0), members[1].FrozenMessageID)
		assert.Equal(t, int64(0), members[1].LastReadMessageID)
	})

	t.Run("handles_empty_members", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		members, err := FuncLoadMembersByRolePaginated(ctx, 10, -3, 10, 0)
		require.NoError(t, err)
		assert.NotNil(t, members)
		assert.Empty(t, members)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		members, err := FuncLoadMembersByRolePaginated(ctx, 10, -3, 10, 0)
		assert.Nil(t, members)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{
				{int64(1), int64(10), int64(101), -3, now.UnixMilli(), 0, nil, nil, now.UnixMilli(), now.UnixMilli()},
			}, errors.New("iter err")), nil
		}, nil)

		members, err := FuncLoadMembersByRolePaginated(ctx, 10, -3, 10, 0)
		assert.Nil(t, members)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
