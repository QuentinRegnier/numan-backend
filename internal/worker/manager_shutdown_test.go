package worker

import (
	"context"
	"testing"
	"time"

	infraTest "github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveWorkerConcurrency(t *testing.T) {
	t.Run("default 64 when unset", func(t *testing.T) {
		t.Setenv("WORKER_CONCURRENCY", "")
		assert.Equal(t, 64, ResolveWorkerConcurrency())
	})

	t.Run("accepts plastic values (16, 128, 256, 512)", func(t *testing.T) {
		for _, tc := range []struct {
			env  string
			want int
		}{
			{"16", 16},
			{"128", 128},
			{"256", 256},
			{"512", 512},
		} {
			t.Setenv("WORKER_CONCURRENCY", tc.env)
			assert.Equal(t, tc.want, ResolveWorkerConcurrency())
		}
	})

	t.Run("clamps above max and falls back on invalid", func(t *testing.T) {
		t.Setenv("WORKER_CONCURRENCY", "4096")
		assert.Equal(t, 1024, ResolveWorkerConcurrency())

		t.Setenv("WORKER_CONCURRENCY", "0")
		assert.Equal(t, 64, ResolveWorkerConcurrency())

		t.Setenv("WORKER_CONCURRENCY", "-10")
		assert.Equal(t, 64, ResolveWorkerConcurrency())

		t.Setenv("WORKER_CONCURRENCY", "abc")
		assert.Equal(t, 64, ResolveWorkerConcurrency())
	})
}

func TestAssignWorkerShards_CoversAll64ShardsAtEveryConcurrency(t *testing.T) {
	for _, workers := range []int{1, 8, 16, 32, 64, 128, 256} {
		coverage := make(map[int]int)
		for w := 0; w < workers; w++ {
			shards := assignWorkerShards(w, workers, redis.QueueShards)
			require.NotEmpty(t, shards)
			for _, s := range shards {
				assert.GreaterOrEqual(t, s, 0)
				assert.Less(t, s, redis.QueueShards)
				coverage[s]++
			}
		}
		assert.Len(t, coverage, redis.QueueShards, "tous les 64 shards doivent être couverts pour workers=%d", workers)
		if workers == 256 {
			for s := 0; s < redis.QueueShards; s++ {
				assert.Equal(t, 4, coverage[s], "à 256 workers, chaque shard doit avoir 4 consommateurs parallèles")
			}
		}
		if workers == 16 {
			for s := 0; s < redis.QueueShards; s++ {
				assert.Equal(t, 1, coverage[s], "à 16 workers, chaque shard doit être couvert exactement une fois")
			}
		}
	}
}

func TestPopSmartBatchBlockingMulti_DrainsAcrossMultipleAssignedShards(t *testing.T) {
	_ = infraTest.SetupTestRedis(t)
	ctx := context.Background()

	// Enqueue 2 événements sur 2 shards distincts puis vérifie qu'un seul worker multi-shards les consomme tous les deux
	require.NoError(t, redis.EnqueueDB(ctx, 101, 101, redis.EntityUser, redis.ActionUpdate, map[string]any{"id": 101}, redis.TargetPostgres))
	require.NoError(t, redis.EnqueueDB(ctx, 202, 202, redis.EntityUser, redis.ActionUpdate, map[string]any{"id": 202}, redis.TargetPostgres))

	s1 := redis.GetShardID(101)
	s2 := redis.GetShardID(202)
	shards := []int{s1}
	if s2 != s1 {
		shards = append(shards, s2)
	}

	var drained []redis.AsyncEvent
	for len(drained) < 2 {
		batch, err := redis.PopSmartBatchBlockingMulti(ctx, shards, 10)
		require.NoError(t, err)
		require.NotEmpty(t, batch)
		drained = append(drained, batch...)
	}
	assert.Len(t, drained, 2)
}

func TestRunWorker_ExitsGracefullyOnContextCancel(t *testing.T) {
	_ = infraTest.SetupTestRedis(t)

	ctx, cancel := context.WithCancel(context.Background())

	workerWG.Add(1)
	go func() {
		defer workerWG.Done()
		runWorker(ctx, 0)
	}()

	// Laisse le worker démarrer puis déclenche SIGTERM simulé (cancel)
	time.Sleep(50 * time.Millisecond)
	cancel()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer waitCancel()
	require.NoError(t, WaitForShutdown(waitCtx))
}

func TestWaitForShutdown_RespectsTimeout(t *testing.T) {
	workerWG.Add(1)
	defer workerWG.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	err := WaitForShutdown(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
