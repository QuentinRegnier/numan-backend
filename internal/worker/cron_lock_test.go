package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunWithDistributedCronLock_PreventsConcurrentDuplicates(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	// Simule un autre pod qui détient déjà le verrou
	token, acquired, err := redis.AcquireCronLock(ctx, "test_dup_cron", 5*time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)

	var runs atomic.Int32
	executed := runWithDistributedCronLock(ctx, "test_dup_cron", 5*time.Minute, func(context.Context) {
		runs.Add(1)
	})
	assert.False(t, executed, "cron must be skipped while another pod holds the lock")
	assert.Equal(t, int32(0), runs.Load())

	// Libère le verrou : le prochain pod peut exécuter le cycle
	require.NoError(t, redis.ReleaseCronLock(ctx, "test_dup_cron", token))

	executed2 := runWithDistributedCronLock(ctx, "test_dup_cron", 5*time.Minute, func(context.Context) {
		runs.Add(1)
	})
	assert.True(t, executed2)
	assert.Equal(t, int32(1), runs.Load())

	// Après exécution, le verrou a bien été libéré pour le cycle suivant
	executed3 := runWithDistributedCronLock(ctx, "test_dup_cron", 5*time.Minute, func(context.Context) {
		runs.Add(1)
	})
	assert.True(t, executed3)
	assert.Equal(t, int32(2), runs.Load())
}
