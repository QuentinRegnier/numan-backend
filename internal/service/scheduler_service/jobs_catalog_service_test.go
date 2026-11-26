package scheduler_service_test

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterAllDefaultJobsCatalog(t *testing.T) {
	_ = test.SetupTestRedis(t)
	eng := scheduler_service.NewEngine("dc-eu-1", scheduler_service.DefaultRegimeConfig(), nil)
	scheduler_service.RegisterAllDefaultJobs(eng)

	ov := eng.StatusSnapshot(context.Background())
	// 8 hygiène + 5 BDD/stockage + 5 algo/cache (incl. redis_orphan_cleanup) + 7 crons migrés = 25 jobs individuels
	require.Len(t, ov.Jobs, 25)

	names := make(map[string]bool, len(ov.Jobs))
	for _, j := range ov.Jobs {
		names[j.Name] = true
		assert.NoError(t, j.Validate())
	}
	assert.True(t, names["pg_expired_sessions_purge"])
	assert.True(t, names["redis_sync_ledger_prune"])
	assert.True(t, names["pg_vacuum_analyze_hot_tables"])
	assert.True(t, names["storage_aborted_multipart_cleanup"])
	assert.True(t, names["most_cache_surgical_purify"])
	assert.True(t, names["object_pool_morning_prewarm"])
	assert.True(t, names["algo_time_decay_deep"])
	assert.True(t, names["redis_orphan_cleanup"])
}
