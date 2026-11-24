package postgres

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresHealthRepository(t *testing.T) {
	t.Run("nil write/read pool returns typed error and false stats", func(t *testing.T) {
		require.Error(t, FuncPingWrite(context.Background()))
		require.Error(t, FuncPingRead(context.Background()))
		_, okW := FuncWritePoolStats()
		assert.False(t, okW)
		_, okR := FuncReadPoolStats()
		assert.False(t, okR)
		assert.False(t, FuncHasDedicatedReadPool())
	})

	t.Run("active pools succeed on ping and return stats", func(t *testing.T) {
		_, cleanup := test.SetupMockDB(t, nil, nil)
		defer cleanup()

		require.NoError(t, FuncPingWrite(context.Background()))
		require.NoError(t, FuncPingRead(context.Background()))
		_, okW := FuncWritePoolStats()
		assert.True(t, okW)
		_, okR := FuncReadPoolStats()
		assert.True(t, okR)
	})

	t.Run("FuncAcquireSchedulerAdvisoryLock acquires and releases via SQL functions", func(t *testing.T) {
		var queries []string
		_, cleanup := test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			queries = append(queries, query)
			return test.NewMockRows([]string{"ok"}, [][]driver.Value{{true}}), nil
		}, nil)
		defer cleanup()

		release, acquired, hasDB, err := FuncAcquireSchedulerAdvisoryLock(context.Background(), 12345)
		require.NoError(t, err)
		assert.True(t, hasDB)
		assert.True(t, acquired)
		require.NotNil(t, release)
		release(context.Background())

		require.Len(t, queries, 2)
		assert.Contains(t, queries[0], "util.func_try_scheduler_advisory_lock")
		assert.Contains(t, queries[1], "util.func_release_scheduler_advisory_lock")
	})
}
