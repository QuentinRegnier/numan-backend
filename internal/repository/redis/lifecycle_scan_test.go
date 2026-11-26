package redis_test

import (
	"context"
	"errors"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanKeysBatchedAndUnlink(t *testing.T) {
	ctx := context.Background()
	_ = test.SetupTestRedis(t)

	for i := 1; i <= 12; i++ {
		require.NoError(t, repoRedis.UsersLite.SetPrimitive(ctx, i, i))
		require.NoError(t, repoRedis.Posts.SetPrimitive(ctx, i, i))
	}

	t.Run("ScanKeysBatchedOnCore streams keys and honors ErrStopScan", func(t *testing.T) {
		var collected []string
		err := repoRedis.ScanKeysBatchedOnCore(ctx, repoRedis.UsersLite.Prefix+"*", 5, func(keys []string) error {
			collected = append(collected, keys...)
			if len(collected) >= 5 {
				return repoRedis.ErrStopScan
			}
			return nil
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(collected), 5)
		assert.LessOrEqual(t, len(collected), 12)
	})

	t.Run("ScanKeysBatchedOnObjects scans all object pool keys and UnlinkKeysOnObjects deletes them", func(t *testing.T) {
		var keys []string
		err := repoRedis.ScanKeysBatchedOnObjects(ctx, repoRedis.Posts.Prefix+"*", 4, func(batch []string) error {
			keys = append(keys, batch...)
			return nil
		})
		require.NoError(t, err)
		assert.Len(t, keys, 12)

		require.NoError(t, repoRedis.UnlinkKeysOnObjects(ctx, keys[:4]...))

		remaining, err := repoRedis.ScanKeysOnObjects(ctx, repoRedis.Posts.Prefix+"*", 20)
		require.NoError(t, err)
		assert.Len(t, remaining, 8)
	})

	t.Run("callback error propagates cleanly", func(t *testing.T) {
		boom := errors.New("boom")
		err := repoRedis.ScanKeysBatchedOnCore(ctx, repoRedis.UsersLite.Prefix+"*", 5, func(keys []string) error {
			return boom
		})
		require.ErrorIs(t, err, boom)
	})

	t.Run("canceled context stops immediately", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		err := repoRedis.ScanKeysBatchedOnCore(cctx, repoRedis.UsersLite.Prefix+"*", 5, func(keys []string) error {
			t.Fatal("callback must not run on canceled context")
			return nil
		})
		require.ErrorIs(t, err, context.Canceled)
	})
}
