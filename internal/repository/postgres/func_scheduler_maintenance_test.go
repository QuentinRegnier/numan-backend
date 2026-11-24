package postgres

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncSchedulerMaintenance_AllFunctions(t *testing.T) {
	ctx := context.Background()

	t.Run("PurgeExpiredSessions", func(t *testing.T) {
		_, cleanup := test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_purge_expired_sessions")
			return test.NewMockRows([]string{"func_purge_expired_sessions"}, [][]driver.Value{{int64(42)}}), nil
		}, nil)
		defer cleanup()

		n, err := FuncPurgeExpiredSessions(ctx, 1700000000000, 5000)
		require.NoError(t, err)
		assert.Equal(t, int64(42), n)
	})

	t.Run("ReconcileExpiredTempBans", func(t *testing.T) {
		_, cleanup := test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_reconcile_expired_temp_bans")
			return test.NewMockRows([]string{"user_id"}, [][]driver.Value{{int64(101)}, {int64(202)}}), nil
		}, nil)
		defer cleanup()

		ids, err := FuncReconcileExpiredTempBans(ctx, 1700000000000, 500)
		require.NoError(t, err)
		assert.Equal(t, []int64{101, 202}, ids)
	})

	t.Run("PurgeOldSoftDeletes", func(t *testing.T) {
		_, cleanup := test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "util.func_purge_old_soft_deletes")
			return test.NewMockRows([]string{"func_purge_old_soft_deletes"}, [][]driver.Value{{int64(19)}}), nil
		}, nil)
		defer cleanup()

		n, err := FuncPurgeOldSoftDeletes(ctx, 1700000000000, 500)
		require.NoError(t, err)
		assert.Equal(t, int64(19), n)
	})

	t.Run("ReconcileDerivedCounters", func(t *testing.T) {
		_, cleanup := test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "util.func_reconcile_derived_counters")
			return test.NewMockRows([]string{"func_reconcile_derived_counters"}, [][]driver.Value{{int64(7)}}), nil
		}, nil)
		defer cleanup()

		n, err := FuncReconcileDerivedCounters(ctx, 200)
		require.NoError(t, err)
		assert.Equal(t, int64(7), n)
	})

	t.Run("VacuumAnalyzeTable_AllowlistAndNeverFull", func(t *testing.T) {
		var executed []string
		_, cleanup := test.SetupMockDB(t, nil, func(query string, args []driver.NamedValue) (driver.Result, error) {
			executed = append(executed, query)
			return driver.RowsAffected(0), nil
		})
		defer cleanup()

		// Disallowed table must be rejected immediately
		err := FuncVacuumAnalyzeTable(ctx, "auth.users; DROP TABLE auth.users", 10*time.Second)
		require.Error(t, err)

		// Allowlisted table runs VACUUM (ANALYZE) and NEVER VACUUM FULL
		require.NoError(t, FuncVacuumAnalyzeTable(ctx, "content.posts", 15*time.Second))
		joined := strings.Join(executed, " | ")
		assert.Contains(t, joined, "SET statement_timeout")
		assert.Contains(t, joined, "VACUUM (ANALYZE) content.posts")
		assert.NotContains(t, strings.ToUpper(joined), "VACUUM FULL")
	})
}
