package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncFilterExistingEntityIDs(t *testing.T) {
	ctx := context.Background()

	t.Run("rejects_unknown_entity", func(t *testing.T) {
		res, err := FuncFilterExistingEntityIDs(ctx, "unknown_table", []int64{1, 2})
		assert.Nil(t, res)
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	})

	t.Run("empty_ids_returns_empty_map_without_db_call", func(t *testing.T) {
		orig := infradb.PostgresDB
		origRead := infradb.PostgresReadDB
		infradb.PostgresDB = nil
		infradb.PostgresReadDB = nil
		defer func() {
			infradb.PostgresDB = orig
			infradb.PostgresReadDB = origRead
		}()

		res, err := FuncFilterExistingEntityIDs(ctx, OrphanEntityUser, nil)
		require.NoError(t, err)
		assert.Empty(t, res)
	})

	t.Run("fails_closed_when_db_uninitialized", func(t *testing.T) {
		orig := infradb.PostgresDB
		origRead := infradb.PostgresReadDB
		infradb.PostgresDB = nil
		infradb.PostgresReadDB = nil
		defer func() {
			infradb.PostgresDB = orig
			infradb.PostgresReadDB = origRead
		}()

		res, err := FuncFilterExistingEntityIDs(ctx, OrphanEntityPostPhysical, []int64{101})
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("returns_existing_ids_map_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "util.func_filter_existing_entity_ids")
			require.Len(t, args, 2)
			assert.Equal(t, OrphanEntityPostActive, args[0].Value)
			return test.NewMockRows([]string{"id"}, [][]driver.Value{
				{int64(101)},
				{int64(303)},
			}), nil
		}, nil)

		res, err := FuncFilterExistingEntityIDs(ctx, OrphanEntityPostActive, []int64{101, 202, 303})
		require.NoError(t, err)
		assert.True(t, res[101])
		assert.False(t, res[202])
		assert.True(t, res[303])
		assert.Len(t, res, 2)
	})

	t.Run("propagates_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("connection reset")
		}, nil)

		res, err := FuncFilterExistingEntityIDs(ctx, OrphanEntityUser, []int64{101})
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("propagates_scan_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows([]string{"id"}, [][]driver.Value{
				{"not-an-int"},
			}), nil
		}, nil)

		res, err := FuncFilterExistingEntityIDs(ctx, OrphanEntityUser, []int64{101})
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
