package scheduler_service_test

import (
	"context"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAcquireJobLock(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	id1 := scheduler_service.SQLAdvisoryLockID("pg_expired_sessions_purge")
	id2 := scheduler_service.SQLAdvisoryLockID("pg_expired_sessions_purge")
	assert.Greater(t, id1, int64(0))
	assert.Equal(t, id1, id2)

	release, acquired, err := scheduler_service.AcquireJobLock(ctx, "job_lock_test", scheduler_models.LockScopeLocalDC, 15*time.Second)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotNil(t, release)

	// Concurrence bloquée tant que le verrou est détenu
	_, acquired2, err2 := scheduler_service.AcquireJobLock(ctx, "job_lock_test", scheduler_models.LockScopeLocalDC, 15*time.Second)
	require.NoError(t, err2)
	assert.False(t, acquired2)

	release(ctx)

	// Réacquisition après libération
	release3, acquired3, err3 := scheduler_service.AcquireJobLock(ctx, "job_lock_test", scheduler_models.LockScopeLocalDC, 15*time.Second)
	require.NoError(t, err3)
	require.True(t, acquired3)
	release3(ctx)
}
