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

func TestFuncLoadMessageIDsPaginated(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		ids, err := FuncLoadMessageIDsPaginated(ctx, 10, 0, 10, "asc", 0)
		assert.Nil(t, ids)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{"id"}

	t.Run("returns_message_ids_successfully_with_frozen_null", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_load_message_ids_paginated")
			require.Len(t, args, 5)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(0), args[1].Value)
			assert.Equal(t, int64(20), args[2].Value)
			assert.Equal(t, "asc", args[3].Value)
			assert.Nil(t, args[4].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(101)},
				{int64(102)},
			}), nil
		}, nil)

		ids, err := FuncLoadMessageIDsPaginated(ctx, 10, 0, 20, "asc", 0)
		require.NoError(t, err)
		assert.Equal(t, []int64{101, 102}, ids)
	})

	t.Run("passes_frozen_id_when_provided", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			require.Len(t, args, 5)
			assert.Equal(t, int64(50), args[4].Value)
			return test.NewMockRows(cols, [][]driver.Value{{int64(101)}}), nil
		}, nil)

		ids, err := FuncLoadMessageIDsPaginated(ctx, 10, 0, 20, "asc", 50)
		require.NoError(t, err)
		assert.Equal(t, []int64{101}, ids)
	})

	t.Run("handles_empty_results", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		ids, err := FuncLoadMessageIDsPaginated(ctx, 10, 0, 20, "asc", 0)
		require.NoError(t, err)
		assert.NotNil(t, ids)
		assert.Empty(t, ids)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query failed")
		}, nil)

		ids, err := FuncLoadMessageIDsPaginated(ctx, 10, 0, 20, "asc", 0)
		assert.Nil(t, ids)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{{int64(101)}}, errors.New("stream err")), nil
		}, nil)

		ids, err := FuncLoadMessageIDsPaginated(ctx, 10, 0, 20, "asc", 0)
		assert.Nil(t, ids)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
