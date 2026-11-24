package scheduler_service_test

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatabaseJobsExecution(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	eng := scheduler_service.NewEngine("dc-eu-1", scheduler_service.DefaultRegimeConfig(), nil)
	scheduler_service.RegisterAllDefaultJobs(eng)

	t.Run("l1_l2_l3_counter_self_healing calls SQL advisory lock and reconciliation function", func(t *testing.T) {
		var queries []string
		_, cleanup := test.SetupMockDB(t, func(query string, _ []driver.NamedValue) (driver.Rows, error) {
			queries = append(queries, query)
			if strings.Contains(query, "func_reconcile_derived_counters") {
				return test.NewMockRows([]string{"fixed"}, [][]driver.Value{{int64(7)}}), nil
			}
			return test.NewMockRows([]string{"ok"}, [][]driver.Value{{true}}), nil
		}, nil)
		defer cleanup()

		st, err := eng.TriggerJob(ctx, "l1_l2_l3_counter_self_healing", true)
		require.NoError(t, err)
		assert.Equal(t, "completed", st.Status)
		require.GreaterOrEqual(t, len(queries), 3)
		assert.Contains(t, queries[0], "func_try_scheduler_advisory_lock")
		assert.Contains(t, queries[1], "func_reconcile_derived_counters")
		assert.Contains(t, queries[2], "func_release_scheduler_advisory_lock")
	})

	t.Run("mongo_l2_hygiene_and_fragmentation_audit handles nil client gracefully", func(t *testing.T) {
		st, err := eng.TriggerJob(ctx, "mongo_l2_hygiene_and_fragmentation_audit", true)
		require.NoError(t, err)
		assert.Equal(t, "completed", st.Status)
	})

	t.Run("storage_aborted_multipart_cleanup handles nil MinIO gracefully", func(t *testing.T) {
		st, err := eng.TriggerJob(ctx, "storage_aborted_multipart_cleanup", true)
		require.NoError(t, err)
		assert.Equal(t, "completed", st.Status)
	})
}
