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

func TestFuncLoadRelationsByDirectionPaginated(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		ids, err := FuncLoadRelationsByDirectionPaginated(ctx, 10, 1, "out", 10, 0)
		assert.Nil(t, ids)
		assertDatabaseError(t, err)
	})

	cols := []string{"matched_id"}

	t.Run("returns_matched_ids_successfully", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_load_relations_paginated_by_direction")
			require.Len(t, args, 5)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(1), args[1].Value)
			assert.Equal(t, "out", args[2].Value)
			assert.Equal(t, int64(10), args[3].Value)
			assert.Equal(t, int64(0), args[4].Value)
			return newMockRows(cols, [][]driver.Value{
				{int64(201)},
				{int64(202)},
			}), nil
		}, nil)

		ids, err := FuncLoadRelationsByDirectionPaginated(ctx, 10, 1, "out", 10, 0)
		require.NoError(t, err)
		assert.Equal(t, []int64{201, 202}, ids)
	})

	t.Run("handles_empty_relations", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		ids, err := FuncLoadRelationsByDirectionPaginated(ctx, 10, 1, "out", 10, 0)
		require.NoError(t, err)
		assert.NotNil(t, ids)
		assert.Empty(t, ids)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query failed")
		}, nil)

		ids, err := FuncLoadRelationsByDirectionPaginated(ctx, 10, 1, "out", 10, 0)
		assert.Nil(t, ids)
		assertDatabaseError(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRowsWithErr(cols, [][]driver.Value{{int64(201)}}, errors.New("iter err")), nil
		}, nil)

		ids, err := FuncLoadRelationsByDirectionPaginated(ctx, 10, 1, "out", 10, 0)
		assert.Nil(t, ids)
		assertDatabaseError(t, err)
	})
}
