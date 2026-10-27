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

func TestFuncLoadQuarantinedMediaPaginated(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UnixMilli()
	cols := []string{"id", "owner_id", "storage_path", "visibility", "created_at", "updated_at"}

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		mediaList, err := FuncLoadQuarantinedMediaPaginated(ctx, 10, 0)
		assert.Nil(t, mediaList)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("returns_quarantined_media_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "moderation.func_load_quarantined_media_paginated")
			require.Len(t, args, 2)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(0), args[1].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(90), int64(10), "uploads/q1.avif", int64(2), now, now},
				{int64(91), int64(11), "uploads/q2.avif", int64(2), now, now},
			}), nil
		}, nil)

		mediaList, err := FuncLoadQuarantinedMediaPaginated(ctx, 10, 0)
		require.NoError(t, err)
		require.Len(t, mediaList, 2)
		assert.Equal(t, int64(90), mediaList[0].ID)
		assert.Equal(t, int64(10), mediaList[0].OwnerID)
		assert.Equal(t, "uploads/q1.avif", mediaList[0].StoragePath)
		assert.Equal(t, 2, int(mediaList[0].Visibility))
	})

	t.Run("handles_empty_results", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		mediaList, err := FuncLoadQuarantinedMediaPaginated(ctx, 10, 0)
		require.NoError(t, err)
		assert.NotNil(t, mediaList)
		assert.Empty(t, mediaList)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db query error")
		}, nil)

		mediaList, err := FuncLoadQuarantinedMediaPaginated(ctx, 10, 0)
		assert.Nil(t, mediaList)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{
				{int64(90), int64(10), "uploads/q1.avif", int64(2), now, now},
			}, errors.New("iteration failed")), nil
		}, nil)

		mediaList, err := FuncLoadQuarantinedMediaPaginated(ctx, 10, 0)
		assert.Nil(t, mediaList)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
