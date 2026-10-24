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

func TestFuncLoadPostIDsByTagPaginated(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		ids, err := FuncLoadPostIDsByTagPaginated(ctx, "tech", 0, 10)
		assert.Nil(t, ids)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{"id"}

	t.Run("returns_post_ids_by_tag_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_load_post_ids_by_tag_paginated")
			require.Len(t, args, 3)
			assert.Equal(t, "tech", args[0].Value)
			assert.Equal(t, int64(0), args[1].Value)
			assert.Equal(t, int64(10), args[2].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(1001)},
				{int64(1002)},
			}), nil
		}, nil)

		ids, err := FuncLoadPostIDsByTagPaginated(ctx, "tech", 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []int64{1001, 1002}, ids)
	})

	t.Run("handles_empty_post_ids", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		ids, err := FuncLoadPostIDsByTagPaginated(ctx, "nonexistent", 0, 10)
		require.NoError(t, err)
		assert.NotNil(t, ids)
		assert.Empty(t, ids)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db failure")
		}, nil)

		ids, err := FuncLoadPostIDsByTagPaginated(ctx, "tech", 0, 10)
		assert.Nil(t, ids)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{{int64(1001)}}, errors.New("iter err")), nil
		}, nil)

		ids, err := FuncLoadPostIDsByTagPaginated(ctx, "tech", 0, 10)
		assert.Nil(t, ids)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
