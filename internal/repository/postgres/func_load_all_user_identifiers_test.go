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

func TestFuncLoadAllUserIdentifiers(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		idents, err := FuncLoadAllUserIdentifiers(ctx)
		assert.Nil(t, idents)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{"username", "email", "phone"}

	t.Run("returns_identifiers_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_load_all_user_identifiers")
			return test.NewMockRows(cols, [][]driver.Value{
				{"alice", "alice@example.com", "+33123456789"},
				{"bob", nil, nil},
			}), nil
		}, nil)

		idents, err := FuncLoadAllUserIdentifiers(ctx)
		require.NoError(t, err)
		require.Len(t, idents, 2)
		require.NotNil(t, idents[0].Username)
		assert.Equal(t, "alice", *idents[0].Username)
		require.NotNil(t, idents[0].Email)
		assert.Equal(t, "alice@example.com", *idents[0].Email)
		require.NotNil(t, idents[0].Phone)
		assert.Equal(t, "+33123456789", *idents[0].Phone)

		require.NotNil(t, idents[1].Username)
		assert.Equal(t, "bob", *idents[1].Username)
		assert.Nil(t, idents[1].Email)
		assert.Nil(t, idents[1].Phone)
	})

	t.Run("handles_empty_identifiers", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		idents, err := FuncLoadAllUserIdentifiers(ctx)
		require.NoError(t, err)
		assert.NotNil(t, idents)
		assert.Empty(t, idents)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		idents, err := FuncLoadAllUserIdentifiers(ctx)
		assert.Nil(t, idents)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{{"alice", nil, nil}}, errors.New("iter err")), nil
		}, nil)

		idents, err := FuncLoadAllUserIdentifiers(ctx)
		assert.Nil(t, idents)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
