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

func TestFuncLoadUsersPaginated(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		users, err := FuncLoadUsersPaginated(ctx, 10, 0)
		assert.Nil(t, users)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{
		"id", "username", "first_name", "last_name", "profile_picture_id",
		"bio", "grade", "conversation_permission", "add_group_permission",
	}

	t.Run("returns_users_paginated_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_load_users_paginated")
			require.Len(t, args, 2)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(0), args[1].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(1), "alice", "Alice", "Dupont", int64(50), "Dev", 1, 0, 1},
				{int64(2), "bob", "Bob", "Martin", nil, nil, 0, 1, 0},
			}), nil
		}, nil)

		users, err := FuncLoadUsersPaginated(ctx, 10, 0)
		require.NoError(t, err)
		require.Len(t, users, 2)
		assert.Equal(t, int64(1), users[0].ID)
		assert.Equal(t, "alice", users[0].Username)
		assert.Equal(t, int64(50), users[0].ProfilePictureID)
		assert.Equal(t, "Dev", users[0].Bio)

		assert.Equal(t, int64(2), users[1].ID)
		assert.Equal(t, int64(0), users[1].ProfilePictureID)
		assert.Equal(t, "", users[1].Bio)
	})

	t.Run("handles_empty_users", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		users, err := FuncLoadUsersPaginated(ctx, 10, 0)
		require.NoError(t, err)
		assert.NotNil(t, users)
		assert.Empty(t, users)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		users, err := FuncLoadUsersPaginated(ctx, 10, 0)
		assert.Nil(t, users)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{
				{int64(1), "alice", "Alice", "Dupont", nil, nil, 1, 0, 0},
			}, errors.New("iter err")), nil
		}, nil)

		users, err := FuncLoadUsersPaginated(ctx, 10, 0)
		assert.Nil(t, users)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
