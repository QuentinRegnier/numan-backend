package cluster_models

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedisSizingModels_Validate(t *testing.T) {
	t.Run("CoreSizingInput", func(t *testing.T) {
		var nilIn *CoreSizingInput
		require.Error(t, nilIn.Validate())
		require.Error(t, (&CoreSizingInput{MostCacheBytes: -1}).Validate())
		require.Error(t, (&CoreSizingInput{ReplicationFactor: 11}).Validate())
		require.NoError(t, (&CoreSizingInput{MostCacheBytes: GiB, ActiveUsers: 1000, ReplicationFactor: 2}).Validate())
	})

	t.Run("NodeRAMProfile", func(t *testing.T) {
		var nilNode *NodeRAMProfile
		require.Error(t, nilNode.Validate())
		require.Error(t, (&NodeRAMProfile{TotalRAMBytes: -1}).Validate())
		require.Error(t, (&NodeRAMProfile{Hostname: strings.Repeat("a", 254)}).Validate())
		require.NoError(t, (&NodeRAMProfile{Hostname: "srv-1", TotalRAMBytes: 64 * GiB}).Validate())
	})

	t.Run("PubSubSizingInput", func(t *testing.T) {
		var nilPS *PubSubSizingInput
		require.Error(t, nilPS.Validate())
		require.Error(t, (&PubSubSizingInput{ActiveWSPods: -1}).Validate())
		require.NoError(t, (&PubSubSizingInput{ActiveWSPods: 2, ConcurrentWSUsers: 10000}).Validate())
	})

	t.Run("TierSizingEstimate", func(t *testing.T) {
		var nilEst *TierSizingEstimate
		require.Error(t, nilEst.Validate())
		require.Error(t, (&TierSizingEstimate{PubSubBytes: -1}).Validate())
		require.NoError(t, (&TierSizingEstimate{CoreUsableBytes: 2 * GiB, CoreClusterTotalBytes: 4 * GiB, PubSubBytes: 256 * MiB}).Validate())
	})
}
