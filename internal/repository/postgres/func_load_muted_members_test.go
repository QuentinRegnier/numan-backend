package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncLoadMutedMembersPaginated(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		members, err := FuncLoadMutedMembersPaginated(ctx, 10, 10, 0)
		assert.Nil(t, members)
		assertDatabaseError(t, err)
	})

	cols := []string{"user_id", "restricted_until"}

	t.Run("returns_muted_members_successfully", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_load_muted_members_paginated")
			require.Len(t, args, 3)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(10), args[1].Value)
			assert.Equal(t, int64(0), args[2].Value)
			return newMockRows(cols, [][]driver.Value{
				{int64(201), int64(1700000000)},
				{int64(202), int64(1750000000)},
			}), nil
		}, nil)

		members, err := FuncLoadMutedMembersPaginated(ctx, 10, 10, 0)
		require.NoError(t, err)
		require.Len(t, members, 2)
		assert.Equal(t, int64(201), members[0].UserID)
		assert.Equal(t, int64(1700000000), members[0].RestrictedUntil)
	})

	t.Run("handles_empty_members", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		members, err := FuncLoadMutedMembersPaginated(ctx, 10, 10, 0)
		require.NoError(t, err)
		assert.NotNil(t, members)
		assert.Empty(t, members)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		members, err := FuncLoadMutedMembersPaginated(ctx, 10, 10, 0)
		assert.Nil(t, members)
		assertDatabaseError(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRowsWithErr(cols, [][]driver.Value{{int64(201), int64(1700000000)}}, errors.New("iter err")), nil
		}, nil)

		members, err := FuncLoadMutedMembersPaginated(ctx, 10, 10, 0)
		assert.Nil(t, members)
		assertDatabaseError(t, err)
	})
}
