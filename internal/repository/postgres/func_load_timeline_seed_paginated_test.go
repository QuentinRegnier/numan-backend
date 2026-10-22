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

func TestFuncLoadTimelineSeedPaginated(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		seeds, err := FuncLoadTimelineSeedPaginated(ctx, 10, 0)
		assert.Nil(t, seeds)
		assertDatabaseError(t, err)
	})

	cols := []string{"post_id", "user_id", "created_at"}
	now := time.Now()

	t.Run("returns_timeline_seed_successfully", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_load_timeline_seed_paginated")
			require.Len(t, args, 2)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(0), args[1].Value)
			return newMockRows(cols, [][]driver.Value{
				{int64(1001), int64(201), now},
				{int64(1002), int64(202), now},
			}), nil
		}, nil)

		seeds, err := FuncLoadTimelineSeedPaginated(ctx, 10, 0)
		require.NoError(t, err)
		require.Len(t, seeds, 2)
		assert.Equal(t, int64(1001), seeds[0].PostID)
		assert.Equal(t, int64(201), seeds[0].UserID)
	})

	t.Run("handles_empty_seeds", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		seeds, err := FuncLoadTimelineSeedPaginated(ctx, 10, 0)
		require.NoError(t, err)
		assert.NotNil(t, seeds)
		assert.Empty(t, seeds)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query failed")
		}, nil)

		seeds, err := FuncLoadTimelineSeedPaginated(ctx, 10, 0)
		assert.Nil(t, seeds)
		assertDatabaseError(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRowsWithErr(cols, [][]driver.Value{
				{int64(1001), int64(201), now},
			}, errors.New("iter err")), nil
		}, nil)

		seeds, err := FuncLoadTimelineSeedPaginated(ctx, 10, 0)
		assert.Nil(t, seeds)
		assertDatabaseError(t, err)
	})
}
