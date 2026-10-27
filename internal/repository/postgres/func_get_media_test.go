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

func TestFuncGetMedia(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		m, err := FuncGetMedia(ctx, 42)
		assert.Empty(t, m.ID)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{"id", "owner_id", "storage_path", "visibility", "created_at", "updated_at"}
	now := time.Now().UnixMilli()

	t.Run("returns_media_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.get_media")
			require.Len(t, args, 1)
			assert.Equal(t, int64(42), args[0].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(42), int64(10), "uploads/photo.jpg", int64(1), now, now},
			}), nil
		}, nil)

		m, err := FuncGetMedia(ctx, 42)
		require.NoError(t, err)
		assert.Equal(t, int64(42), m.ID)
		assert.Equal(t, int64(10), m.OwnerID)
		assert.Equal(t, "uploads/photo.jpg", m.StoragePath)
		assert.Equal(t, 1, int(m.Visibility))
	})

	t.Run("returns_quarantined_media_for_service_inspection", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(42), int64(10), "uploads/photo.jpg", int64(2), now, now},
			}), nil
		}, nil)

		m, err := FuncGetMedia(ctx, 42)
		require.NoError(t, err)
		assert.Equal(t, 2, int(m.Visibility))
	})

	t.Run("handles_soft_deleted_media", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(42), int64(10), "uploads/photo.jpg", int64(-1), now, now},
			}), nil
		}, nil)

		m, err := FuncGetMedia(ctx, 42)
		assert.Equal(t, -1, int(m.Visibility))
		test.AssertNotFoundError_Postgres(t, err, "MEDIA_DELETED")
	})

	t.Run("handles_rejected_moderated_media", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(42), int64(10), "uploads/photo.jpg", int64(-2), now, now},
			}), nil
		}, nil)

		m, err := FuncGetMedia(ctx, 42)
		assert.Equal(t, -2, int(m.Visibility))
		require.Error(t, err)
	})

	t.Run("handles_media_not_found", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		m, err := FuncGetMedia(ctx, 999)
		assert.Empty(t, m.ID)
		test.AssertNotFoundError_Postgres(t, err, "MEDIA_NOT_FOUND")
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db failure")
		}, nil)

		m, err := FuncGetMedia(ctx, 42)
		assert.Empty(t, m.ID)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
