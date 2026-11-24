package cluster_models

import (
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

const (
	KiB int64 = 1024
	MiB int64 = 1024 * KiB
	GiB int64 = 1024 * MiB
)

// CoreSizingInput regroupe les paramètres de dimensionnement du Tier 1 (Redis Core — HA répliqué).
type CoreSizingInput struct {
	MostCacheBytes     int64 `json:"most_cache_bytes"`
	SpeedCacheBytes    int64 `json:"speed_cache_bytes"`
	ActiveUsers        int64 `json:"active_users"`
	MaxQueueDepth      int64 `json:"max_queue_depth"`
	TelemetryOverheadB int64 `json:"telemetry_overhead_bytes"`
	ReplicationFactor  int   `json:"replication_factor"`
}

// Validate vérifie les bornes métier des paramètres de dimensionnement Redis Core.
func (in *CoreSizingInput) Validate() error {
	if in == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres Redis Core sont vides.", nil)
	}
	if in.MostCacheBytes < 0 || in.SpeedCacheBytes < 0 || in.ActiveUsers < 0 || in.MaxQueueDepth < 0 || in.TelemetryOverheadB < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres de dimensionnement Redis Core ne peuvent pas être négatifs.", nil)
	}
	if in.ReplicationFactor < 0 || in.ReplicationFactor > 10 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le facteur de réplication Redis Core doit être compris entre 0 et 10.", nil)
	}
	return nil
}

// NodeRAMProfile décrit la capacité RAM d'un serveur physique participant au Tier 2 Object Pool.
type NodeRAMProfile struct {
	Hostname          string `json:"hostname"`
	TotalRAMBytes     int64  `json:"total_ram_bytes"`
	OSReserveBytes    int64  `json:"os_reserve_bytes"`
	CoreAllocBytes    int64  `json:"core_alloc_bytes"`
	AppAllocBytes     int64  `json:"app_alloc_bytes"`
	SafetyOffsetBytes int64  `json:"safety_offset_bytes"`
}

// Validate vérifie les bornes métier du profil RAM d'un serveur physique.
func (n *NodeRAMProfile) Validate() error {
	if n == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le profil RAM du serveur est vide.", nil)
	}
	if len(strings.TrimSpace(n.Hostname)) > 253 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom d'hôte ne peut pas dépasser 253 caractères.", nil)
	}
	if n.TotalRAMBytes < 0 || n.OSReserveBytes < 0 || n.CoreAllocBytes < 0 || n.AppAllocBytes < 0 || n.SafetyOffsetBytes < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les volumes RAM du serveur ne peuvent pas être négatifs.", nil)
	}
	return nil
}

// PubSubSizingInput décrit la charge WebSocket et Pub/Sub du datacenter pour Tier 3.
type PubSubSizingInput struct {
	ActiveWSPods       int64 `json:"active_ws_pods"`
	ConcurrentWSUsers  int64 `json:"concurrent_ws_users"`
	PresenceKeys       int64 `json:"presence_keys"`
	FanoutBurstBufferB int64 `json:"fanout_burst_buffer_bytes"`
}

// Validate vérifie les bornes métier des paramètres de dimensionnement Redis Pub/Sub.
func (in *PubSubSizingInput) Validate() error {
	if in == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres Redis Pub/Sub sont vides.", nil)
	}
	if in.ActiveWSPods < 0 || in.ConcurrentWSUsers < 0 || in.PresenceKeys < 0 || in.FanoutBurstBufferB < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres de dimensionnement Redis Pub/Sub ne peuvent pas être négatifs.", nil)
	}
	return nil
}

// TierSizingEstimate représente le résultat déterministe du dimensionnement des 3 tiers Redis.
type TierSizingEstimate struct {
	CoreUsableBytes        int64            `json:"core_usable_bytes"`
	CoreClusterTotalBytes  int64            `json:"core_cluster_total_bytes"`
	ObjectPoolPerNodeBytes map[string]int64 `json:"object_pool_per_node_bytes"`
	ObjectPoolTotalBytes   int64            `json:"object_pool_total_bytes"`
	PubSubBytes            int64            `json:"pubsub_bytes"`
}

// Validate vérifie les invariants de l'estimation de dimensionnement.
func (e *TierSizingEstimate) Validate() error {
	if e == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'estimation Redis est vide.", nil)
	}
	if e.CoreUsableBytes < 0 || e.CoreClusterTotalBytes < 0 || e.ObjectPoolTotalBytes < 0 || e.PubSubBytes < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les volumes estimés ne peuvent pas être négatifs.", nil)
	}
	return nil
}
