package cluster_service

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/cluster_models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisSizingService(t *testing.T) {
	t.Run("EstimateCoreBytes enforces 2GiB floor and rejects invalid input", func(t *testing.T) {
		_, err := EstimateCoreBytes(cluster_models.CoreSizingInput{MostCacheBytes: -1})
		require.Error(t, err)

		small, err := EstimateCoreBytes(cluster_models.CoreSizingInput{ActiveUsers: 100})
		require.NoError(t, err)
		assert.Equal(t, 2*cluster_models.GiB, small)

		large, err := EstimateCoreBytes(cluster_models.CoreSizingInput{
			MostCacheBytes:  3 * cluster_models.GiB,
			SpeedCacheBytes: 1 * cluster_models.GiB,
			ActiveUsers:     500_000,
			MaxQueueDepth:   100_000,
		})
		require.NoError(t, err)
		assert.Greater(t, large, 4*cluster_models.GiB)
	})

	t.Run("EstimateRedis3TierSizingService aggregates all 3 tiers cleanly", func(t *testing.T) {
		est, err := EstimateRedis3TierSizingService(
			cluster_models.CoreSizingInput{
				MostCacheBytes:    4 * cluster_models.GiB,
				SpeedCacheBytes:   2 * cluster_models.GiB,
				ActiveUsers:       200_000,
				MaxQueueDepth:     50_000,
				ReplicationFactor: 2,
			},
			[]cluster_models.NodeRAMProfile{
				{
					Hostname:          "srv-worker-1",
					TotalRAMBytes:     64 * cluster_models.GiB,
					OSReserveBytes:    4 * cluster_models.GiB,
					CoreAllocBytes:    8 * cluster_models.GiB,
					AppAllocBytes:     12 * cluster_models.GiB,
					SafetyOffsetBytes: 4 * cluster_models.GiB,
				},
				{
					Hostname:          "srv-api-1",
					TotalRAMBytes:     32 * cluster_models.GiB,
					OSReserveBytes:    4 * cluster_models.GiB,
					AppAllocBytes:     30 * cluster_models.GiB,
					SafetyOffsetBytes: 4 * cluster_models.GiB,
				},
			},
			cluster_models.PubSubSizingInput{
				ActiveWSPods:      4,
				ConcurrentWSUsers: 50_000,
				PresenceKeys:      100_000,
			},
		)
		require.NoError(t, err)
		assert.Equal(t, est.CoreUsableBytes*2, est.CoreClusterTotalBytes)
		assert.Equal(t, 36*cluster_models.GiB, est.ObjectPoolPerNodeBytes["srv-worker-1"])
		assert.Equal(t, int64(0), est.ObjectPoolPerNodeBytes["srv-api-1"])
		assert.Equal(t, 36*cluster_models.GiB, est.ObjectPoolTotalBytes)
		assert.Greater(t, est.PubSubBytes, 256*cluster_models.MiB)
	})

	t.Run("EstimateRedis3TierSizingService rejects invalid node or pubsub input", func(t *testing.T) {
		_, err := EstimateRedis3TierSizingService(
			cluster_models.CoreSizingInput{},
			[]cluster_models.NodeRAMProfile{{Hostname: "bad", TotalRAMBytes: -10}},
			cluster_models.PubSubSizingInput{},
		)
		require.Error(t, err)

		_, err = EstimateRedis3TierSizingService(
			cluster_models.CoreSizingInput{},
			nil,
			cluster_models.PubSubSizingInput{ActiveWSPods: -1},
		)
		require.Error(t, err)
	})
}
