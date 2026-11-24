package cluster_service

import (
	"math"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/cluster_models"
)

// EstimateCoreBytes calcule la RAM utilisable requise pour Tier 1 Redis Core :
// max(2 GiB, MostCacheEst + SpeedCacheEst + ActiveUsers*2KiB + MaxQueueDepth*2KiB + TelemetryOverhead)
func EstimateCoreBytes(in cluster_models.CoreSizingInput) (int64, error) {
	if err := in.Validate(); err != nil {
		return 0, err
	}
	const minCore = 2 * cluster_models.GiB
	telemetry := in.TelemetryOverheadB
	if telemetry <= 0 {
		telemetry = 16 * cluster_models.MiB
	}
	total := in.MostCacheBytes + in.SpeedCacheBytes + in.ActiveUsers*2*cluster_models.KiB + in.MaxQueueDepth*2*cluster_models.KiB + telemetry
	if total < minCore {
		return minCore, nil
	}
	return total, nil
}

// EstimateNodeObjectPoolSpareBytes calcule la RAM excédentaire récoltable sur un nœud pour Tier 2 Object Pool :
// max(0, TotalRAM - OSReserve - CoreAlloc - AppAlloc - SafetyOffset)
func EstimateNodeObjectPoolSpareBytes(node cluster_models.NodeRAMProfile) (int64, error) {
	if err := node.Validate(); err != nil {
		return 0, err
	}
	spare := node.TotalRAMBytes -
		node.OSReserveBytes -
		node.CoreAllocBytes -
		node.AppAllocBytes -
		node.SafetyOffsetBytes
	if spare < 0 {
		return 0, nil
	}
	return spare, nil
}

// EstimateObjectPoolBytes agrège la RAM 100% utilisable (non répliquée) sur l'ensemble des nœuds participants.
func EstimateObjectPoolBytes(nodes []cluster_models.NodeRAMProfile) (int64, map[string]int64, error) {
	perNode := make(map[string]int64, len(nodes))
	var total int64
	for _, n := range nodes {
		spare, err := EstimateNodeObjectPoolSpareBytes(n)
		if err != nil {
			return 0, nil, err
		}
		name := n.Hostname
		if name == "" {
			name = "unnamed-node"
		}
		perNode[name] = spare
		total += spare
	}
	return total, perNode, nil
}

// EstimatePubSubBytes calcule la RAM requise pour Tier 3 Redis Pub/Sub :
// max(256 MiB, ActiveWSPods*64MiB + (ConcurrentWSUsers/10000)*96MiB + PresenceKeys*256B + FanoutBurstBuffer)
func EstimatePubSubBytes(in cluster_models.PubSubSizingInput) (int64, error) {
	if err := in.Validate(); err != nil {
		return 0, err
	}
	const minPubSub = 256 * cluster_models.MiB
	burst := in.FanoutBurstBufferB
	if burst <= 0 {
		burst = 64 * cluster_models.MiB
	}
	wsUserBytes := int64(math.Ceil((float64(in.ConcurrentWSUsers) / 10000.0) * float64(96*cluster_models.MiB)))
	total := in.ActiveWSPods*64*cluster_models.MiB + wsUserBytes + in.PresenceKeys*256 + burst
	if total < minPubSub {
		return minPubSub, nil
	}
	return total, nil
}

// EstimateRedis3TierSizingService calcule le dimensionnement déterministe des 3 tiers Redis d'un datacenter.
func EstimateRedis3TierSizingService(
	core cluster_models.CoreSizingInput,
	nodes []cluster_models.NodeRAMProfile,
	pubsub cluster_models.PubSubSizingInput,
) (cluster_models.TierSizingEstimate, error) {
	coreUsable, err := EstimateCoreBytes(core)
	if err != nil {
		return cluster_models.TierSizingEstimate{}, err
	}
	objTotal, perNode, err := EstimateObjectPoolBytes(nodes)
	if err != nil {
		return cluster_models.TierSizingEstimate{}, err
	}
	psBytes, err := EstimatePubSubBytes(pubsub)
	if err != nil {
		return cluster_models.TierSizingEstimate{}, err
	}

	repFactor := core.ReplicationFactor
	if repFactor < 1 {
		repFactor = 2
	}

	out := cluster_models.TierSizingEstimate{
		CoreUsableBytes:        coreUsable,
		CoreClusterTotalBytes:  coreUsable * int64(repFactor),
		ObjectPoolPerNodeBytes: perNode,
		ObjectPoolTotalBytes:   objTotal,
		PubSubBytes:            psBytes,
	}
	return out, out.Validate()
}
