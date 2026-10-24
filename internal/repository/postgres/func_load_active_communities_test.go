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

func TestFuncLoadActiveCommunities(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		comms, err := FuncLoadActiveCommunities(ctx)
		assert.Nil(t, comms)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{"id", "name", "description", "avatar_id", "member_count"}

	t.Run("returns_active_communities_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_load_active_communities")
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(1), "Go Enthusiasts", "All things Go", int64(100), 500},
				{int64(2), "Backend Devs", "Architectures", int64(101), 250},
			}), nil
		}, nil)

		comms, err := FuncLoadActiveCommunities(ctx)
		require.NoError(t, err)
		require.Len(t, comms, 2)
		assert.Equal(t, "Go Enthusiasts", comms[0].Name)
		assert.Equal(t, 500, comms[0].MemberCount)
	})

	t.Run("handles_empty_communities", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		comms, err := FuncLoadActiveCommunities(ctx)
		require.NoError(t, err)
		assert.NotNil(t, comms)
		assert.Empty(t, comms)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		comms, err := FuncLoadActiveCommunities(ctx)
		assert.Nil(t, comms)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{
				{int64(1), "Go", "Desc", int64(100), 10},
			}, errors.New("iter err")), nil
		}, nil)

		comms, err := FuncLoadActiveCommunities(ctx)
		assert.Nil(t, comms)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
