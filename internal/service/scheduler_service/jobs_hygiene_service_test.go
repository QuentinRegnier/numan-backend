package scheduler_service_test

import (
	"context"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHygieneJobsExecution(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	eng := scheduler_service.NewEngine("dc-eu-1", scheduler_service.DefaultRegimeConfig(), nil)
	scheduler_service.RegisterAllDefaultJobs(eng)

	// Préparer des entrées anciennes dans UserSyncLedger et une arête Markov morte dans GraphEdges
	oldMs := float64(domain.NowMillis() - int64((40*24*time.Hour)/time.Millisecond))
	recentMs := float64(domain.NowMillis())
	ledgerKey := repoRedis.UserSyncLedger.Key(101)
	require.NoError(t, repoRedis.ZAddOnCore(ctx, ledgerKey, oldMs, "old_evt"))
	require.NoError(t, repoRedis.ZAddOnCore(ctx, ledgerKey, recentMs, "recent_evt"))

	edgeKey := repoRedis.GraphEdges.Key("tag_a")
	require.NoError(t, repoRedis.ZAddOnCore(ctx, edgeKey, 0.01, "dead_tag"))

	st1, err := eng.TriggerJob(ctx, "redis_sync_ledger_prune", true)
	require.NoError(t, err)
	assert.Equal(t, "completed", st1.Status)

	remaining, err := repoRedis.ZRangeOnCore(ctx, ledgerKey, 0, -1)
	require.NoError(t, err)
	assert.Equal(t, []string{"recent_evt"}, remaining)

	st2, err := eng.TriggerJob(ctx, "redis_graph_edges_prune", true)
	require.NoError(t, err)
	assert.Equal(t, "completed", st2.Status)
	card, err := repoRedis.ZCardOnCore(ctx, edgeKey)
	require.NoError(t, err)
	assert.Equal(t, int64(0), card)

	st3, err := eng.TriggerJob(ctx, "redis_lex_autocomplete_audit", true)
	require.NoError(t, err)
	assert.Equal(t, "completed", st3.Status)

	st4, err := eng.TriggerJob(ctx, "dlq_audit_and_replay", true)
	require.NoError(t, err)
	assert.Equal(t, "completed", st4.Status)
}
