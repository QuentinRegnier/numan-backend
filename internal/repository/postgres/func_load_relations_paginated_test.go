package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncLoadRelationsPaginated(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		rels, err := FuncLoadRelationsPaginated(ctx, 10, 0)
		assert.Nil(t, rels)
		assertDatabaseError(t, err)
	})

	cols := []string{"caller_id", "target_id", "state", "created_at"}
	now := time.Now()

	t.Run("returns_relations_successfully", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_load_relations_paginated")
			require.Len(t, args, 2)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(0), args[1].Value)
			return newMockRows(cols, [][]driver.Value{
				{int64(1), int64(2), 1, now},
				{int64(3), int64(4), 2, now},
			}), nil
		}, nil)

		rels, err := FuncLoadRelationsPaginated(ctx, 10, 0)
		require.NoError(t, err)
		require.Len(t, rels, 2)
		assert.Equal(t, int64(1), rels[0].CallerID)
		assert.Equal(t, int64(2), rels[0].TargetID)
		assert.Equal(t, 1, rels[0].State)
	})

	t.Run("handles_empty_relations", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		rels, err := FuncLoadRelationsPaginated(ctx, 10, 0)
		require.NoError(t, err)
		assert.NotNil(t, rels)
		assert.Empty(t, rels)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db failure")
		}, nil)

		rels, err := FuncLoadRelationsPaginated(ctx, 10, 0)
		assert.Nil(t, rels)
		assertDatabaseError(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRowsWithErr(cols, [][]driver.Value{{int64(1), int64(2), 1, now}}, errors.New("iter err")), nil
		}, nil)

		rels, err := FuncLoadRelationsPaginated(ctx, 10, 0)
		assert.Nil(t, rels)
		assertDatabaseError(t, err)
	})
}
